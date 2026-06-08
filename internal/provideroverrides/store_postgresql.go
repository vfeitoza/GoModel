package provideroverrides

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQLStore stores provider overrides in PostgreSQL.
type PostgreSQLStore struct {
	pool *pgxpool.Pool
}

// NewPostgreSQLStore creates the provider_overrides table and indexes if needed.
func NewPostgreSQLStore(ctx context.Context, pool *pgxpool.Pool) (*PostgreSQLStore, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if pool == nil {
		return nil, fmt.Errorf("connection pool is required")
	}

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS provider_overrides (
			provider_name TEXT PRIMARY KEY,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider_overrides table: %w", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_provider_overrides_updated_at ON provider_overrides(updated_at DESC)`); err != nil {
		return nil, fmt.Errorf("failed to create provider_overrides updated_at index: %w", err)
	}
	return &PostgreSQLStore{pool: pool}, nil
}

// List returns all provider overrides sorted by provider name.
func (s *PostgreSQLStore) List(ctx context.Context) ([]ProviderOverride, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT provider_name, enabled, created_at, updated_at
		FROM provider_overrides
		ORDER BY provider_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list provider overrides: %w", err)
	}
	defer rows.Close()

	var result []ProviderOverride
	for rows.Next() {
		var providerName string
		var enabled bool
		var createdAt, updatedAt int64
		if err := rows.Scan(&providerName, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan provider override: %w", err)
		}
		result = append(result, ProviderOverride{
			ProviderName: providerName,
			Enabled:      enabled,
			CreatedAt:    time.Unix(createdAt, 0).UTC(),
			UpdatedAt:    time.Unix(updatedAt, 0).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider overrides: %w", err)
	}
	return result, nil
}

// Upsert creates or updates a provider override.
func (s *PostgreSQLStore) Upsert(ctx context.Context, override ProviderOverride) error {
	normalized := normalizeStoredOverride(override)
	now := time.Now().UTC()
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now

	_, err := s.pool.Exec(ctx, `
		INSERT INTO provider_overrides (provider_name, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider_name) DO UPDATE SET
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, normalized.ProviderName, normalized.Enabled, normalized.CreatedAt.Unix(), normalized.UpdatedAt.Unix())
	if err != nil {
		return fmt.Errorf("upsert provider override: %w", err)
	}
	return nil
}

// Delete removes a provider override by name.
func (s *PostgreSQLStore) Delete(ctx context.Context, providerName string) error {
	providerName = normalizeProviderName(providerName)
	if providerName == "" {
		return fmt.Errorf("provider_name is required")
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM provider_overrides WHERE provider_name = $1`, providerName)
	if err != nil {
		return fmt.Errorf("delete provider override: %w", err)
	}
	return nil
}

// Close releases resources held by the store.
func (s *PostgreSQLStore) Close(ctx context.Context) error {
	return nil // PostgreSQL connection is managed externally
}