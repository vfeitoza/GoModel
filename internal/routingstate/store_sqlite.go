package routingstate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS routing_state (
			key TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			provider_name TEXT NOT NULL DEFAULT '',
			canonical_model TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create routing_state table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_routing_state_kind ON routing_state(kind)`); err != nil {
		return nil, fmt.Errorf("failed to create routing_state kind index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_routing_state_provider_name ON routing_state(provider_name)`); err != nil {
		return nil, fmt.Errorf("failed to create routing_state provider_name index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_routing_state_canonical_model ON routing_state(canonical_model)`); err != nil {
		return nil, fmt.Errorf("failed to create routing_state canonical_model index: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) List(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
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
		entry, err := scanSQLiteEntry(rows)
		return entry, true, err
	}, rows.Err)
}

func (s *SQLiteStore) Upsert(ctx context.Context, entry Entry) error {
	entry, err := normalizeEntry(entry)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO routing_state (key, kind, provider_name, canonical_model, model, enabled, reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			kind = excluded.kind,
			provider_name = excluded.provider_name,
			canonical_model = excluded.canonical_model,
			model = excluded.model,
			enabled = excluded.enabled,
			reason = excluded.reason,
			updated_at = excluded.updated_at
	`,
		entry.Key,
		string(entry.Kind),
		entry.ProviderName,
		entry.CanonicalModel,
		entry.Model,
		boolToInt(entry.Enabled),
		entry.Reason,
		entry.CreatedAt.Unix(),
		entry.UpdatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("upsert routing state: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Delete(ctx context.Context, key string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM routing_state WHERE key = ?`, strings.TrimSpace(key))
	if err != nil {
		return fmt.Errorf("delete routing state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read delete rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) Close() error { return nil }

func scanSQLiteEntry(scanner interface{ Scan(dest ...any) error }) (Entry, error) {
	var entry Entry
	var enabled int
	var createdAt int64
	var updatedAt int64
	if err := scanner.Scan(
		&entry.Key,
		&entry.Kind,
		&entry.ProviderName,
		&entry.CanonicalModel,
		&entry.Model,
		&enabled,
		&entry.Reason,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Entry{}, fmt.Errorf("scan routing state: %w", err)
	}
	entry.Enabled = enabled != 0
	entry.CreatedAt = time.Unix(createdAt, 0).UTC()
	entry.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return entry, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
