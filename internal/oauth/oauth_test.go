package oauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gomodel/internal/oauth"
)

// --- PKCE tests ---

func TestNewPKCEPair(t *testing.T) {
	pair1, err := oauth.NewPKCEPair()
	require.NoError(t, err)
	assert.NotEmpty(t, pair1.Verifier)
	assert.NotEmpty(t, pair1.Challenge)
	assert.NotEqual(t, pair1.Verifier, pair1.Challenge)

	// Two pairs must be distinct
	pair2, err := oauth.NewPKCEPair()
	require.NoError(t, err)
	assert.NotEqual(t, pair1.Verifier, pair2.Verifier)
	assert.NotEqual(t, pair1.Challenge, pair2.Challenge)
}

func TestNewState(t *testing.T) {
	s1, err := oauth.NewState()
	require.NoError(t, err)
	assert.NotEmpty(t, s1)

	s2, err := oauth.NewState()
	require.NoError(t, err)
	assert.NotEqual(t, s1, s2)
}

// --- AnthropicProvider tests ---

func TestAnthropicProvider_AuthorizationURL(t *testing.T) {
	p := oauth.NewAnthropicProvider()
	pair, err := oauth.NewPKCEPair()
	require.NoError(t, err)

	authURL := p.AuthorizationURL("test-state", pair.Verifier, oauth.LocalCallbackURI(54545))

	assert.Contains(t, authURL, "claude.ai/oauth/authorize")
	assert.Contains(t, authURL, oauth.AnthropicClientID)
	assert.Contains(t, authURL, "test-state")
	assert.Contains(t, authURL, "code_challenge_method=S256")
	assert.Contains(t, authURL, "localhost%3A54545") // encoded port in redirect_uri
}

func TestAnthropicProvider_ExchangeCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "authorization_code", body["grant_type"])
		assert.Equal(t, "test-code", body["code"])
		assert.Equal(t, "test-verifier", body["code_verifier"])
		assert.Equal(t, "test-state", body["state"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":      "access-abc",
			"refresh_token":     "refresh-xyz",
			"expires_in":        3600,
			"scope":             "org:create_api_key user:profile user:inference",
			"subscription_type": "pro",
		})
	}))
	defer srv.Close()

	p := oauth.NewAnthropicProviderWithTestTokenURL(srv.URL)
	resp, err := p.ExchangeCode(context.Background(), "test-code", "test-verifier", "test-state", oauth.LocalCallbackURI(54545))
	require.NoError(t, err)

	assert.Equal(t, "access-abc", resp.AccessToken)
	assert.Equal(t, "refresh-xyz", resp.RefreshToken)
	assert.Equal(t, 3600, resp.ExpiresIn)
	assert.Equal(t, "pro", resp.SubscriptionType)
	assert.Contains(t, resp.Scopes, "user:profile")
}

func TestAnthropicProvider_ExchangeCode_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	p := oauth.NewAnthropicProviderWithTestTokenURL(srv.URL)
	_, err := p.ExchangeCode(context.Background(), "bad-code", "verifier", "state", oauth.LocalCallbackURI(54545))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAnthropicProvider_RefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "refresh_token", body["grant_type"])
		assert.Equal(t, "old-refresh", body["refresh_token"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	p := oauth.NewAnthropicProviderWithTestTokenURL(srv.URL)
	resp, err := p.RefreshToken(context.Background(), "old-refresh")
	require.NoError(t, err)

	assert.Equal(t, "new-access", resp.AccessToken)
	assert.Equal(t, "new-refresh", resp.RefreshToken)
}

func TestAnthropicProvider_RefreshToken_PreservesOriginalWhenNotRotated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Provider does not return a new refresh token
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	p := oauth.NewAnthropicProviderWithTestTokenURL(srv.URL)
	resp, err := p.RefreshToken(context.Background(), "original-refresh")
	require.NoError(t, err)

	// Original refresh token must be preserved
	assert.Equal(t, "original-refresh", resp.RefreshToken)
}

func TestAnthropicProvider_RefreshToken_EmptyToken(t *testing.T) {
	p := oauth.NewAnthropicProvider()
	_, err := p.RefreshToken(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refresh token is required")
}

func TestAnthropicProvider_FetchProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"account": map[string]any{
				"uuid":            "acc-123",
				"email":           "user@example.com",
				"full_name":       "Test User",
				"has_claude_pro":  true,
				"has_claude_max":  false,
			},
			"organization": map[string]any{
				"name": "Test Org",
			},
		})
	}))
	defer srv.Close()

	p := oauth.NewAnthropicProviderWithTestProfileURL(srv.URL)
	profile, err := p.FetchProfile(context.Background(), "test-token")
	require.NoError(t, err)

	assert.Equal(t, "acc-123", profile.AccountID)
	assert.Equal(t, "user@example.com", profile.Email)
	assert.Equal(t, "Test User", profile.DisplayName)
	assert.Equal(t, "pro", profile.SubscriptionType)
	assert.True(t, profile.HasClaudePro)
	assert.False(t, profile.HasClaudeMax)
	assert.Equal(t, "Test Org", profile.OrganizationName)
}

// --- CallbackServer tests ---

func TestCallbackServer_ReceivesCode(t *testing.T) {
	cs := oauth.NewCallbackServer(0) // OS picks port
	port, err := cs.Start()
	require.NoError(t, err)
	assert.Greater(t, port, 0)

	// Simulate the OAuth provider redirecting back
	go func() {
		time.Sleep(50 * time.Millisecond)
		resp, err := http.Get(strings.Replace(
			"http://localhost:PORT/callback?code=auth-code-123&state=csrf-state",
			"PORT", strconv.Itoa(port), 1,
		))
		if err == nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := cs.Wait(ctx)
	require.NoError(t, err)
	assert.Equal(t, "auth-code-123", result.Code)
	assert.Equal(t, "csrf-state", result.State)
}

func TestCallbackServer_ErrorFromProvider(t *testing.T) {
	cs := oauth.NewCallbackServer(0)
	port, err := cs.Start()
	require.NoError(t, err)

	go func() {
		time.Sleep(50 * time.Millisecond)
		resp, err := http.Get(strings.Replace(
			"http://localhost:PORT/callback?error=access_denied",
			"PORT", strconv.Itoa(port), 1,
		))
		if err == nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cs.Wait(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_denied")
}

func TestCallbackServer_ContextCancelled(t *testing.T) {
	cs := oauth.NewCallbackServer(0)
	_, err := cs.Start()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = cs.Wait(ctx)
	require.Error(t, err)
}
