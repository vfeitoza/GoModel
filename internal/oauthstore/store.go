// Package oauthstore provides persistence for OAuth tokens used by providers
// configured with api_key: "oauth".
package oauthstore

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	// ErrNotFound indicates no OAuth token exists for the given provider name.
	ErrNotFound = errors.New("oauth token not found")
)

// Token holds a persisted OAuth token for a named provider instance.
type Token struct {
	ProviderName     string    // configured provider name (e.g. "anthropic_oauth")
	ProviderType     string    // provider type (e.g. "anthropic")
	AccessToken      string    // current bearer token
	RefreshToken     string    // used to obtain a new access token; may be empty
	ExpiresAt        time.Time // when the access token expires
	Scopes           []string  // granted OAuth scopes
	AccountEmail     string    // authenticated account email
	AccountID        string    // provider account/org ID
	DisplayName      string    // human-readable account name
	SubscriptionType string    // e.g. "free", "pro", "max"
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// IsExpired reports whether the access token has expired, using a 5-minute
// safety margin so callers can refresh before the token actually expires.
func (t *Token) IsExpired() bool {
	if t == nil {
		return true
	}
	return time.Now().Add(5 * time.Minute).After(t.ExpiresAt)
}

// Store defines persistence operations for OAuth tokens.
type Store interface {
	// Save creates or replaces the token for the given provider name.
	Save(ctx context.Context, token *Token) error
	// Get returns the token for the given provider name, or ErrNotFound.
	Get(ctx context.Context, providerName string) (*Token, error)
	// Delete removes the token for the given provider name.
	// Returns nil if the token did not exist.
	Delete(ctx context.Context, providerName string) error
	// List returns all stored tokens ordered by provider name.
	List(ctx context.Context) ([]*Token, error)
	// Close releases any resources held by the store.
	Close() error
}

// normalizeProviderName trims and lowercases the provider name for consistent
// storage and lookup.
func normalizeProviderName(name string) string {
	return strings.TrimSpace(name)
}

// joinScopes serialises a scope slice to a space-separated string.
func joinScopes(scopes []string) string {
	filtered := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s != "" {
			filtered = append(filtered, s)
		}
	}
	return strings.Join(filtered, " ")
}

// splitScopes deserialises a space-separated scope string.
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
