package provideroverrides

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoProviderOverrideDocument struct {
	ProviderName string    `bson:"_id"`
	Enabled      bool      `bson:"enabled"`
	CreatedAt    time.Time `bson:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at"`
}

// MongoDBStore stores provider overrides in MongoDB.
type MongoDBStore struct {
	collection *mongo.Collection
}

// NewMongoDBStore creates collection indexes if needed.
func NewMongoDBStore(database *mongo.Database) (*MongoDBStore, error) {
	if database == nil {
		return nil, fmt.Errorf("database is required")
	}
	coll := database.Collection("provider_overrides")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "updated_at", Value: -1}}},
	}
	if _, err := coll.Indexes().CreateMany(ctx, indexes); err != nil {
		return nil, fmt.Errorf("create provider_overrides indexes: %w", err)
	}
	return &MongoDBStore{collection: coll}, nil
}

// List returns all provider overrides sorted by provider name.
func (s *MongoDBStore) List(ctx context.Context) ([]ProviderOverride, error) {
	cursor, err := s.collection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list provider overrides: %w", err)
	}
	defer cursor.Close(ctx)

	var result []ProviderOverride
	for cursor.Next(ctx) {
		var doc mongoProviderOverrideDocument
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode provider override: %w", err)
		}
		result = append(result, ProviderOverride{
			ProviderName: doc.ProviderName,
			Enabled:      doc.Enabled,
			CreatedAt:    doc.CreatedAt,
			UpdatedAt:    doc.UpdatedAt,
		})
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider overrides: %w", err)
	}
	return result, nil
}

// Upsert creates or updates a provider override.
func (s *MongoDBStore) Upsert(ctx context.Context, override ProviderOverride) error {
	normalized := normalizeStoredOverride(override)
	now := time.Now().UTC()
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now

	doc := mongoProviderOverrideDocument{
		ProviderName: normalized.ProviderName,
		Enabled:      normalized.Enabled,
		CreatedAt:    normalized.CreatedAt,
		UpdatedAt:    normalized.UpdatedAt,
	}

	opts := options.UpdateOne().SetUpsert(true)
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": normalized.ProviderName},
		bson.M{"$set": doc},
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert provider override: %w", err)
	}
	return nil
}

// Delete removes a provider override by name.
func (s *MongoDBStore) Delete(ctx context.Context, providerName string) error {
	providerName = normalizeProviderName(providerName)
	if providerName == "" {
		return fmt.Errorf("provider_name is required")
	}
	_, err := s.collection.DeleteOne(ctx, bson.M{"_id": providerName})
	if err != nil {
		return fmt.Errorf("delete provider override: %w", err)
	}
	return nil
}

// Close releases resources held by the store.
func (s *MongoDBStore) Close(ctx context.Context) error {
	return nil // MongoDB connection is managed externally
}