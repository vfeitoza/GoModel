package oauthstore_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"gomodel/internal/oauthstore"
)

func newTestSQLiteStore(t *testing.T) oauthstore.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	store, err := oauthstore.NewSQLiteStore(db)
	require.NoError(t, err)
	return store
}

func sampleToken(providerName string) *oauthstore.Token {
	return &oauthstore.Token{
		ProviderName:     providerName,
		ProviderType:     "anthropic",
		AccessToken:      "access-token-abc",
		RefreshToken:     "refresh-token-xyz",
		ExpiresAt:        time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		Scopes:           []string{"org:create_api_key", "user:profile", "user:inference"},
		AccountEmail:     "user@example.com",
		AccountID:        "acc-123",
		DisplayName:      "Test User",
		SubscriptionType: "pro",
	}
}

func TestSQLiteStore_SaveAndGet(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	token := sampleToken("anthropic_oauth")
	require.NoError(t, store.Save(ctx, token))

	got, err := store.Get(ctx, "anthropic_oauth")
	require.NoError(t, err)

	assert.Equal(t, token.ProviderName, got.ProviderName)
	assert.Equal(t, token.ProviderType, got.ProviderType)
	assert.Equal(t, token.AccessToken, got.AccessToken)
	assert.Equal(t, token.RefreshToken, got.RefreshToken)
	assert.Equal(t, token.ExpiresAt.Unix(), got.ExpiresAt.Unix())
	assert.Equal(t, token.Scopes, got.Scopes)
	assert.Equal(t, token.AccountEmail, got.AccountEmail)
	assert.Equal(t, token.AccountID, got.AccountID)
	assert.Equal(t, token.DisplayName, got.DisplayName)
	assert.Equal(t, token.SubscriptionType, got.SubscriptionType)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestSQLiteStore_Save_UpdatesExisting(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	token := sampleToken("anthropic_oauth")
	require.NoError(t, store.Save(ctx, token))

	first, err := store.Get(ctx, "anthropic_oauth")
	require.NoError(t, err)
	originalCreatedAt := first.CreatedAt

	// Update the token
	token.AccessToken = "new-access-token"
	token.AccountEmail = "other@example.com"
	require.NoError(t, store.Save(ctx, token))

	updated, err := store.Get(ctx, "anthropic_oauth")
	require.NoError(t, err)

	assert.Equal(t, "new-access-token", updated.AccessToken)
	assert.Equal(t, "other@example.com", updated.AccountEmail)
	// created_at must be preserved
	assert.Equal(t, originalCreatedAt.Unix(), updated.CreatedAt.Unix())
}

func TestSQLiteStore_Get_NotFound(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	assert.ErrorIs(t, err, oauthstore.ErrNotFound)
}

func TestSQLiteStore_Delete(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	token := sampleToken("anthropic_oauth")
	require.NoError(t, store.Save(ctx, token))

	require.NoError(t, store.Delete(ctx, "anthropic_oauth"))

	_, err := store.Get(ctx, "anthropic_oauth")
	assert.ErrorIs(t, err, oauthstore.ErrNotFound)
}

func TestSQLiteStore_Delete_NonExistent(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Deleting a non-existent token should not error
	assert.NoError(t, store.Delete(ctx, "nonexistent"))
}

func TestSQLiteStore_List(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	require.NoError(t, store.Save(ctx, sampleToken("provider_b")))
	require.NoError(t, store.Save(ctx, sampleToken("provider_a")))
	require.NoError(t, store.Save(ctx, sampleToken("provider_c")))

	tokens, err := store.List(ctx)
	require.NoError(t, err)
	require.Len(t, tokens, 3)

	// Should be ordered by provider_name ASC
	assert.Equal(t, "provider_a", tokens[0].ProviderName)
	assert.Equal(t, "provider_b", tokens[1].ProviderName)
	assert.Equal(t, "provider_c", tokens[2].ProviderName)
}

func TestSQLiteStore_List_Empty(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	tokens, err := store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, tokens)
}

func TestToken_IsExpired(t *testing.T) {
	t.Run("not expired", func(t *testing.T) {
		token := &oauthstore.Token{ExpiresAt: time.Now().Add(time.Hour)}
		assert.False(t, token.IsExpired())
	})

	t.Run("expired", func(t *testing.T) {
		token := &oauthstore.Token{ExpiresAt: time.Now().Add(-time.Minute)}
		assert.True(t, token.IsExpired())
	})

	t.Run("within safety margin", func(t *testing.T) {
		// Expires in 3 minutes — within the 5-minute safety margin
		token := &oauthstore.Token{ExpiresAt: time.Now().Add(3 * time.Minute)}
		assert.True(t, token.IsExpired())
	})

	t.Run("nil token", func(t *testing.T) {
		var token *oauthstore.Token
		assert.True(t, token.IsExpired())
	})
}
