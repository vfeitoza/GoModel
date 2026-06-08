package provideroverrides

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SQLiteStore stores provider overrides in SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates the provider_overrides table and indexes if needed.
func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS provider_overrides (
			provider_name TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider_overrides table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_provider_overrides_updated_at ON provider_overrides(updated_at DESC)`); err != nil {
		return nil, fmt.Errorf("failed to create provider_overrides updated_at index: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// List returns all provider overrides sorted by provider name.
func (s *SQLiteStore) List(ctx context.Context) ([]ProviderOverride, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_name, enabled, created_at, updated_at
		FROM provider_overrides
		ORDER BY provider_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list provider overrides: %w", err)
	}
	defer rows.Close()
	return scanProviderOverrides(rows)
}

// Upsert creates or updates a provider override.
func (s *SQLiteStore) Upsert(ctx context.Context, override ProviderOverride) error {
	normalized := normalizeStoredOverride(override)
	now := time.Now().UTC()
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_overrides (provider_name, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(provider_name) DO UPDATE SET
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, normalized.ProviderName, normalized.Enabled, normalized.CreatedAt.Unix(), normalized.UpdatedAt.Unix())
	if err != nil {
		return fmt.Errorf("upsert provider override: %w", err)
	}
	return nil
}

// Delete removes a provider override by name.
func (s *SQLiteStore) Delete(ctx context.Context, providerName string) error {
	providerName = normalizeProviderName(providerName)
	if providerName == "" {
		return fmt.Errorf("provider_name is required")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM provider_overrides WHERE provider_name = ?`, providerName)
	if err != nil {
		return fmt.Errorf("delete provider override: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Close releases resources held by the store.
func (s *SQLiteStore) Close(ctx context.Context) error {
	return nil // SQLite connection is managed externally
}

// scanProviderOverrides scans rows into a slice of ProviderOverride.
func scanProviderOverrides(rows *sql.Rows) ([]ProviderOverride, error) {
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

// ErrNotFound is returned when a provider override does not exist.
var ErrNotFound = errors.New("provider override not found")