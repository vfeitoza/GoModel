// Package oauth provides OAuth 2.0 with PKCE support for provider authentication.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// ErrStateMismatch is returned when the OAuth state parameter does not match.
var ErrStateMismatch = errors.New("oauth state mismatch")

// ErrCallbackTimeout is returned when the local callback server times out.
var ErrCallbackTimeout = errors.New("oauth callback timeout")

// TokenResponse holds the tokens returned by the OAuth token endpoint.
type TokenResponse struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        int // seconds
	Scopes           []string
	SubscriptionType string
}

// Profile holds the authenticated user's profile information.
type Profile struct {
	AccountID        string
	Email            string
	DisplayName      string
	SubscriptionType string
	HasClaudePro     bool
	HasClaudeMax     bool
	OrganizationName string
}

// Provider defines the operations needed to complete an OAuth flow.
type Provider interface {
	// AuthorizationURL returns the URL the user must visit to authorize.
	// state is a random CSRF token; verifier is the PKCE code verifier.
	AuthorizationURL(state, verifier string, callbackPort int) string

	// ExchangeCode exchanges an authorization code for tokens.
	ExchangeCode(ctx context.Context, code, verifier, state string, callbackPort int) (*TokenResponse, error)

	// RefreshToken obtains a new access token using a refresh token.
	RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)

	// FetchProfile retrieves the authenticated user's profile.
	FetchProfile(ctx context.Context, accessToken string) (*Profile, error)
}

// generateVerifier creates a cryptographically random PKCE code verifier.
func generateVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// deriveChallenge computes the S256 PKCE code challenge from a verifier.
func deriveChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// generateState creates a random CSRF state token as a hex string,
// matching the format expected by the Anthropic OAuth endpoint.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// PKCEPair holds a verifier and its derived challenge.
type PKCEPair struct {
	Verifier  string
	Challenge string
}

// NewPKCEPair generates a fresh PKCE verifier/challenge pair.
func NewPKCEPair() (PKCEPair, error) {
	verifier, err := generateVerifier()
	if err != nil {
		return PKCEPair{}, err
	}
	return PKCEPair{
		Verifier:  verifier,
		Challenge: deriveChallenge(verifier),
	}, nil
}

// NewState generates a random CSRF state token.
func NewState() (string, error) {
	return generateState()
}
