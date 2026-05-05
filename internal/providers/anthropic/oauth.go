package anthropic

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"gomodel/internal/oauth"
	"gomodel/internal/oauthstore"
)


// oauthState holds the runtime OAuth state for a provider instance.
type oauthState struct {
	mu           sync.Mutex
	store        oauthstore.Store
	providerName string
	oauthProv    *oauth.AnthropicProvider
}

// isOAuthAPIKey reports whether the given api_key value signals OAuth mode.
// The sentinel value is "oauth" (case-insensitive, trimmed).
func isOAuthAPIKey(apiKey string) bool {
	return strings.EqualFold(strings.TrimSpace(apiKey), "oauth")
}

// getValidAccessToken returns a valid access token, refreshing it if needed.
// It is safe for concurrent use.
func (s *oauthState) getValidAccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, err := s.store.Get(ctx, s.providerName)
	if err != nil {
		if err == oauthstore.ErrNotFound {
			return "", fmt.Errorf(
				"provider %q requires OAuth authentication — visit the dashboard to authenticate",
				s.providerName,
			)
		}
		return "", fmt.Errorf("oauth: failed to load token for %q: %w", s.providerName, err)
	}

	if !token.IsExpired() {
		return token.AccessToken, nil
	}

	// Token expired — attempt refresh.
	if token.RefreshToken == "" {
		return "", fmt.Errorf(
			"oauth: access token for %q has expired and no refresh token is available — re-authenticate via the dashboard",
			s.providerName,
		)
	}

	slog.Info("oauth: refreshing access token", "provider", s.providerName)
	resp, err := s.oauthProv.RefreshToken(ctx, token.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("oauth: token refresh failed for %q: %w", s.providerName, err)
	}

	token.AccessToken = resp.AccessToken
	if resp.RefreshToken != "" {
		token.RefreshToken = resp.RefreshToken
	}
	if resp.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	token.UpdatedAt = time.Now()

	if err := s.store.Save(ctx, token); err != nil {
		// Log but don't fail — the token is still usable for this request.
		slog.Warn("oauth: failed to persist refreshed token", "provider", s.providerName, "error", err)
	}

	return token.AccessToken, nil
}

// setOAuthHeader sets the Authorization header using the stored OAuth token.
// If the token is unavailable (e.g. revoked), it cancels the request context
// so the llmclient aborts the upstream call immediately.
func (p *Provider) setOAuthHeader(req *http.Request) {
	token, err := p.oauth.getValidAccessToken(req.Context())
	if err != nil {
		slog.Error("oauth: cannot obtain access token", "provider", p.oauth.providerName, "error", err)
		// Store the error in the request context so callers can surface it,
		// then cancel the context to abort the upstream HTTP call.
		ctx, cancel := context.WithCancelCause(req.Context())
		cancel(err)
		*req = *req.WithContext(ctx)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}
