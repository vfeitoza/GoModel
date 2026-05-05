package oauthstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SQLiteStore stores OAuth tokens in SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates the oauth_tokens table and indexes if needed.
func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS oauth_tokens (
			provider_name     TEXT PRIMARY KEY,
			provider_type     TEXT NOT NULL DEFAULT '',
			access_token      TEXT NOT NULL,
			refresh_token     TEXT NOT NULL DEFAULT '',
			expires_at        INTEGER NOT NULL,
			scopes            TEXT NOT NULL DEFAULT '',
			account_email     TEXT NOT NULL DEFAULT '',
			account_id        TEXT NOT NULL DEFAULT '',
			display_name      TEXT NOT NULL DEFAULT '',
			subscription_type TEXT NOT NULL DEFAULT '',
			created_at        INTEGER NOT NULL,
			updated_at        INTEGER NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create oauth_tokens table: %w", err)
	}

	for _, index := range []string{
		`CREATE INDEX IF NOT EXISTS idx_oauth_tokens_expires ON oauth_tokens(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_tokens_type ON oauth_tokens(provider_type)`,
	} {
		if _, err := db.Exec(index); err != nil {
			return nil, fmt.Errorf("failed to create oauth_tokens index: %w", err)
		}
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Save(ctx context.Context, token *Token) error {
	if token == nil {
		return fmt.Errorf("token is required")
	}
	name := normalizeProviderName(token.ProviderName)
	if name == "" {
		return fmt.Errorf("provider_name is required")
	}

	now := time.Now().UTC()
	createdAt := now
	// Preserve original created_at if the record already exists.
	existing, err := s.Get(ctx, name)
	if err == nil {
		createdAt = existing.CreatedAt
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO oauth_tokens
			(provider_name, provider_type, access_token, refresh_token, expires_at,
			 scopes, account_email, account_id, display_name, subscription_type,
			 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider_name) DO UPDATE SET
			provider_type     = excluded.provider_type,
			access_token      = excluded.access_token,
			refresh_token     = excluded.refresh_token,
			expires_at        = excluded.expires_at,
			scopes            = excluded.scopes,
			account_email     = excluded.account_email,
			account_id        = excluded.account_id,
			display_name      = excluded.display_name,
			subscription_type = excluded.subscription_type,
			updated_at        = excluded.updated_at
	`,
		name,
		strings.TrimSpace(token.ProviderType),
		token.AccessToken,
		token.RefreshToken,
		token.ExpiresAt.UTC().Unix(),
		joinScopes(token.Scopes),
		strings.TrimSpace(token.AccountEmail),
		strings.TrimSpace(token.AccountID),
		strings.TrimSpace(token.DisplayName),
		strings.TrimSpace(token.SubscriptionType),
		createdAt.Unix(),
		now.Unix(),
	)
	if err != nil {
		return fmt.Errorf("save oauth token: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Get(ctx context.Context, providerName string) (*Token, error) {
	name := normalizeProviderName(providerName)
	if name == "" {
		return nil, fmt.Errorf("provider_name is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT provider_name, provider_type, access_token, refresh_token, expires_at,
		       scopes, account_email, account_id, display_name, subscription_type,
		       created_at, updated_at
		FROM oauth_tokens
		WHERE provider_name = ?
	`, name)

	token, err := scanSQLiteToken(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get oauth token: %w", err)
	}
	return token, nil
}

func (s *SQLiteStore) Delete(ctx context.Context, providerName string) error {
	name := normalizeProviderName(providerName)
	if name == "" {
		return fmt.Errorf("provider_name is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE provider_name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete oauth token: %w", err)
	}
	return nil
}

func (s *SQLiteStore) List(ctx context.Context) ([]*Token, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_name, provider_type, access_token, refresh_token, expires_at,
		       scopes, account_email, account_id, display_name, subscription_type,
		       created_at, updated_at
		FROM oauth_tokens
		ORDER BY provider_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list oauth tokens: %w", err)
	}
	defer rows.Close()

	result := make([]*Token, 0)
	for rows.Next() {
		token, err := scanSQLiteToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scan oauth token: %w", err)
		}
		result = append(result, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oauth tokens: %w", err)
	}
	return result, nil
}

func (s *SQLiteStore) Close() error {
	return nil
}

type sqliteScanner interface {
	Scan(dest ...any) error
}

func scanSQLiteToken(scanner sqliteScanner) (*Token, error) {
	return scanTokenRow(scanner)
}
