package routingstate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgreSQLStore struct {
	pool *pgxpool.Pool
}

func NewPostgreSQLStore(ctx context.Context, pool *pgxpool.Pool) (*PostgreSQLStore, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if pool == nil {
		return nil, fmt.Errorf("connection pool is required")
	}

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS routing_state (
			key TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			provider_name TEXT NOT NULL DEFAULT '',
			canonical_model TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create routing_state table: %w", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_routing_state_kind ON routing_state(kind)`); err != nil {
		return nil, fmt.Errorf("failed to create routing_state kind index: %w", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_routing_state_provider_name ON routing_state(provider_name)`); err != nil {
		return nil, fmt.Errorf("failed to create routing_state provider_name index: %w", err)
	}
	if _, err := pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_routing_state_canonical_model ON routing_state(canonical_model)`); err != nil {
		return nil, fmt.Errorf("failed to create routing_state canonical_model index: %w", err)
	}
	return &PostgreSQLStore{pool: pool}, nil
}

func (s *PostgreSQLStore) List(ctx context.Context) ([]Entry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, kind, provider_name, canonical_model, model, enabled, reason, created_at, updated_at
		FROM routing_state
		ORDER BY key ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list routing state: %w", err)
	}
	defer rows.Close()
	return collectEntries(func() (Entry, bool, error) {
		if !rows.Next() {
			return Entry{}, false, nil
		}
		entry, err := scanPostgreSQLEntry(rows)
		return entry, true, err
	}, rows.Err)
}

func (s *PostgreSQLStore) Upsert(ctx context.Context, entry Entry) error {
	entry, err := normalizeEntry(entry)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO routing_state (key, kind, provider_name, canonical_model, model, enabled, reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT(key) DO UPDATE SET
			kind = excluded.kind,
			provider_name = excluded.provider_name,
			canonical_model = excluded.canonical_model,
			model = excluded.model,
			enabled = excluded.enabled,
			reason = excluded.reason,
			updated_at = excluded.updated_at
	`, entry.Key, string(entry.Kind), entry.ProviderName, entry.CanonicalModel, entry.Model, entry.Enabled, entry.Reason, entry.CreatedAt.Unix(), entry.UpdatedAt.Unix())
	if err != nil {
		return fmt.Errorf("upsert routing state: %w", err)
	}
	return nil
}

func (s *PostgreSQLStore) Delete(ctx context.Context, key string) error {
	cmd, err := s.pool.Exec(ctx, `DELETE FROM routing_state WHERE key = $1`, strings.TrimSpace(key))
	if err != nil {
		return fmt.Errorf("delete routing state: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgreSQLStore) Close() error { return nil }

func scanPostgreSQLEntry(scanner interface{ Scan(dest ...any) error }) (Entry, error) {
	var entry Entry
	var createdAt int64
	var updatedAt int64
	if err := scanner.Scan(&entry.Key, &entry.Kind, &entry.ProviderName, &entry.CanonicalModel, &entry.Model, &entry.Enabled, &entry.Reason, &createdAt, &updatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return Entry{}, ErrNotFound
		}
		return Entry{}, fmt.Errorf("scan routing state: %w", err)
	}
	entry.CreatedAt = time.Unix(createdAt, 0).UTC()
	entry.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return entry, nil
}
