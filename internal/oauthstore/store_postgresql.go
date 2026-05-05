package oauthstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQLStore stores OAuth tokens in PostgreSQL.
type PostgreSQLStore struct {
	pool *pgxpool.Pool
}

// NewPostgreSQLStore creates the oauth_tokens table and indexes if needed.
func NewPostgreSQLStore(ctx context.Context, pool *pgxpool.Pool) (*PostgreSQLStore, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if pool == nil {
		return nil, fmt.Errorf("connection pool is required")
	}

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS oauth_tokens (
			provider_name     TEXT PRIMARY KEY,
			provider_type     TEXT NOT NULL DEFAULT '',
			access_token      TEXT NOT NULL,
			refresh_token     TEXT NOT NULL DEFAULT '',
			expires_at        BIGINT NOT NULL,
			scopes            TEXT NOT NULL DEFAULT '',
			account_email     TEXT NOT NULL DEFAULT '',
			account_id        TEXT NOT NULL DEFAULT '',
			display_name      TEXT NOT NULL DEFAULT '',
			subscription_type TEXT NOT NULL DEFAULT '',
			created_at        BIGINT NOT NULL,
			updated_at        BIGINT NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create oauth_tokens table: %w", err)
	}

	for _, index := range []string{
		`CREATE INDEX IF NOT EXISTS idx_oauth_tokens_expires ON oauth_tokens(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_tokens_type ON oauth_tokens(provider_type)`,
	} {
		if _, err := pool.Exec(ctx, index); err != nil {
			return nil, fmt.Errorf("failed to create oauth_tokens index: %w", err)
		}
	}

	return &PostgreSQLStore{pool: pool}, nil
}

func (s *PostgreSQLStore) Save(ctx context.Context, token *Token) error {
	if token == nil {
		return fmt.Errorf("token is required")
	}
	name := normalizeProviderName(token.ProviderName)
	if name == "" {
		return fmt.Errorf("provider_name is required")
	}

	now := time.Now().UTC()
	createdAt := now
	existing, err := s.Get(ctx, name)
	if err == nil {
		createdAt = existing.CreatedAt
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO oauth_tokens
			(provider_name, provider_type, access_token, refresh_token, expires_at,
			 scopes, account_email, account_id, display_name, subscription_type,
			 created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (provider_name) DO UPDATE SET
			provider_type     = EXCLUDED.provider_type,
			access_token      = EXCLUDED.access_token,
			refresh_token     = EXCLUDED.refresh_token,
			expires_at        = EXCLUDED.expires_at,
			scopes            = EXCLUDED.scopes,
			account_email     = EXCLUDED.account_email,
			account_id        = EXCLUDED.account_id,
			display_name      = EXCLUDED.display_name,
			subscription_type = EXCLUDED.subscription_type,
			updated_at        = EXCLUDED.updated_at
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

func (s *PostgreSQLStore) Get(ctx context.Context, providerName string) (*Token, error) {
	name := normalizeProviderName(providerName)
	if name == "" {
		return nil, fmt.Errorf("provider_name is required")
	}

	row := s.pool.QueryRow(ctx, `
		SELECT provider_name, provider_type, access_token, refresh_token, expires_at,
		       scopes, account_email, account_id, display_name, subscription_type,
		       created_at, updated_at
		FROM oauth_tokens
		WHERE provider_name = $1
	`, name)

	token, err := scanPostgreSQLToken(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get oauth token: %w", err)
	}
	return token, nil
}

func (s *PostgreSQLStore) Delete(ctx context.Context, providerName string) error {
	name := normalizeProviderName(providerName)
	if name == "" {
		return fmt.Errorf("provider_name is required")
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM oauth_tokens WHERE provider_name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete oauth token: %w", err)
	}
	return nil
}

func (s *PostgreSQLStore) List(ctx context.Context) ([]*Token, error) {
	rows, err := s.pool.Query(ctx, `
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
		token, err := scanPostgreSQLToken(rows)
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

func (s *PostgreSQLStore) Close() error {
	return nil
}

type pgScanner interface {
	Scan(dest ...any) error
}

func scanPostgreSQLToken(scanner pgScanner) (*Token, error) {
	return scanTokenRow(scanner)
}
