package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// AnthropicClientID is the public OAuth client ID for Claude/Anthropic.
	// This is the same client ID used by Claude Code and other first-party tools.
	AnthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

	anthropicAuthURL    = "https://claude.ai/oauth/authorize"
	anthropicTokenURL   = "https://console.anthropic.com/v1/oauth/token"
	anthropicProfileURL = "https://api.anthropic.com/api/oauth/profile"

	// DefaultCallbackPort is the preferred local port for the OAuth callback server.
	DefaultCallbackPort = 54545

	anthropicDefaultScopes = "org:create_api_key user:profile user:inference"
)

// AnthropicProvider implements Provider for Anthropic OAuth.
type AnthropicProvider struct {
	httpClient *http.Client
	tokenURL   string // overridable for tests
	profileURL string // overridable for tests
}

// NewAnthropicProvider creates an AnthropicProvider using the default HTTP client.
func NewAnthropicProvider() *AnthropicProvider {
	return &AnthropicProvider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		tokenURL:   anthropicTokenURL,
		profileURL: anthropicProfileURL,
	}
}

// NewAnthropicProviderWithClient creates an AnthropicProvider with a custom HTTP client.
func NewAnthropicProviderWithClient(client *http.Client) *AnthropicProvider {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &AnthropicProvider{
		httpClient: client,
		tokenURL:   anthropicTokenURL,
		profileURL: anthropicProfileURL,
	}
}

// AuthorizationURL builds the Anthropic OAuth authorization URL.
// Parameter order matches the reference implementation (cligate/claude-oauth.js).
func (p *AnthropicProvider) AuthorizationURL(state, verifier, redirectURI string) string {
	challenge := deriveChallenge(verifier)

	// Build params in the exact order used by the reference Claude OAuth client.
	// url.Values.Encode() sorts alphabetically which may differ from what the
	// Anthropic endpoint expects, so we construct the query string manually.
	query := "code=true" +
		"&client_id=" + url.QueryEscape(AnthropicClientID) +
		"&redirect_uri=" + url.QueryEscape(redirectURI) +
		"&response_type=code" +
		"&scope=" + url.QueryEscape(anthropicDefaultScopes) +
		"&state=" + url.QueryEscape(state) +
		"&code_challenge=" + url.QueryEscape(challenge) +
		"&code_challenge_method=S256"

	return anthropicAuthURL + "?" + query
}

// ExchangeCode exchanges an authorization code for tokens.
// Claude requires state in the token exchange body (non-standard).
func (p *AnthropicProvider) ExchangeCode(ctx context.Context, code, verifier, state, redirectURI string) (*TokenResponse, error) {
	body := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectURI,
		"client_id":     AnthropicClientID,
		"code_verifier": verifier,
	}
	if state != "" {
		body["state"] = state
	}

	resp, err := p.postJSON(ctx, p.tokenURL, body)
	if err != nil {
		return nil, fmt.Errorf("anthropic token exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("anthropic token exchange failed (%d): %s", resp.StatusCode, string(raw))
	}

	var payload struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Scope            string `json:"scope"`
		SubscriptionType string `json:"subscription_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode anthropic token response: %w", err)
	}
	if payload.AccessToken == "" {
		return nil, fmt.Errorf("anthropic token exchange: no access_token in response")
	}

	scopes := splitScopes(payload.Scope)
	if len(scopes) == 0 {
		scopes = splitScopes(anthropicDefaultScopes)
	}

	return &TokenResponse{
		AccessToken:      payload.AccessToken,
		RefreshToken:     payload.RefreshToken,
		ExpiresIn:        payload.ExpiresIn,
		Scopes:           scopes,
		SubscriptionType: payload.SubscriptionType,
	}, nil
}

// RefreshToken obtains a new access token using a refresh token.
func (p *AnthropicProvider) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, fmt.Errorf("refresh token is required")
	}

	body := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     AnthropicClientID,
	}

	resp, err := p.postJSON(ctx, p.tokenURL, body)
	if err != nil {
		return nil, fmt.Errorf("anthropic token refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("anthropic token refresh failed (%d): %s", resp.StatusCode, string(raw))
	}

	var payload struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		Scope            string `json:"scope"`
		SubscriptionType string `json:"subscription_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode anthropic refresh response: %w", err)
	}
	if payload.AccessToken == "" {
		return nil, fmt.Errorf("anthropic token refresh: no access_token in response")
	}

	scopes := splitScopes(payload.Scope)
	if len(scopes) == 0 {
		scopes = splitScopes(anthropicDefaultScopes)
	}

	// Preserve the original refresh token if the provider did not rotate it.
	newRefresh := payload.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken
	}

	return &TokenResponse{
		AccessToken:      payload.AccessToken,
		RefreshToken:     newRefresh,
		ExpiresIn:        payload.ExpiresIn,
		Scopes:           scopes,
		SubscriptionType: payload.SubscriptionType,
	}, nil
}

// FetchProfile retrieves the authenticated user's profile from Anthropic.
func (p *AnthropicProvider) FetchProfile(ctx context.Context, accessToken string) (*Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.profileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch anthropic profile: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("fetch anthropic profile failed (%d): %s", resp.StatusCode, string(raw))
	}

	var payload struct {
		Account struct {
			UUID          string `json:"uuid"`
			Email         string `json:"email"`
			FullName      string `json:"full_name"`
			HasClaudePro  bool   `json:"has_claude_pro"`
			HasClaudeMax  bool   `json:"has_claude_max"`
		} `json:"account"`
		Organization struct {
			Name string `json:"name"`
		} `json:"organization"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode anthropic profile: %w", err)
	}

	subscriptionType := "free"
	switch {
	case payload.Account.HasClaudeMax:
		subscriptionType = "max"
	case payload.Account.HasClaudePro:
		subscriptionType = "pro"
	}

	return &Profile{
		AccountID:        payload.Account.UUID,
		Email:            payload.Account.Email,
		DisplayName:      payload.Account.FullName,
		SubscriptionType: subscriptionType,
		HasClaudePro:     payload.Account.HasClaudePro,
		HasClaudeMax:     payload.Account.HasClaudeMax,
		OrganizationName: payload.Organization.Name,
	}, nil
}

// postJSON sends a JSON POST request and returns the raw response.
func (p *AnthropicProvider) postJSON(ctx context.Context, endpoint string, body map[string]string) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return p.httpClient.Do(req)
}

// LocalCallbackURI builds the local redirect URI for the given port.
// Use this when GoModel is running on the same machine as the browser.
func LocalCallbackURI(port int) string {
	return fmt.Sprintf("http://localhost:%d/callback", port)
}

// splitScopes splits a space-separated scope string into a slice.
func splitScopes(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Fields(s)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
