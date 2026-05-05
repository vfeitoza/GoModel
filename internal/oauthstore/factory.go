package oauthstore

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"gomodel/internal/storage"
)

// NewFromStorage creates an oauthstore.Store backed by the given shared storage connection.
// It supports SQLite, PostgreSQL, and MongoDB backends.
func NewFromStorage(ctx context.Context, shared storage.Storage) (Store, error) {
	return storage.ResolveBackend[Store](
		shared,
		func(db *sql.DB) (Store, error) { return NewSQLiteStore(db) },
		func(pool *pgxpool.Pool) (Store, error) { return NewPostgreSQLStore(ctx, pool) },
		func(db *mongo.Database) (Store, error) { return NewMongoDBStore(db) },
	)
}
