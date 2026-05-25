package routingstate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoEntryDocument struct {
	ID             string    `bson:"_id"`
	Kind           string    `bson:"kind"`
	ProviderName   string    `bson:"provider_name,omitempty"`
	CanonicalModel string    `bson:"canonical_model,omitempty"`
	Model          string    `bson:"model,omitempty"`
	Enabled        bool      `bson:"enabled"`
	Reason         string    `bson:"reason,omitempty"`
	CreatedAt      time.Time `bson:"created_at"`
	UpdatedAt      time.Time `bson:"updated_at"`
}

type mongoEntryIDFilter struct {
	ID string `bson:"_id"`
}

type MongoDBStore struct {
	collection *mongo.Collection
}

func NewMongoDBStore(database *mongo.Database) (*MongoDBStore, error) {
	if database == nil {
		return nil, fmt.Errorf("database is required")
	}
	coll := database.Collection("routing_state")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "kind", Value: 1}}},
		{Keys: bson.D{{Key: "provider_name", Value: 1}}},
		{Keys: bson.D{{Key: "canonical_model", Value: 1}}},
	}
	if _, err := coll.Indexes().CreateMany(ctx, indexes); err != nil {
		return nil, fmt.Errorf("create routing_state indexes: %w", err)
	}
	return &MongoDBStore{collection: coll}, nil
}

func (s *MongoDBStore) List(ctx context.Context) ([]Entry, error) {
	cursor, err := s.collection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("list routing state: %w", err)
	}
	defer cursor.Close(ctx)
	result := make([]Entry, 0)
	for cursor.Next(ctx) {
		var doc mongoEntryDocument
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode routing state: %w", err)
		}
		result = append(result, entryFromMongo(doc))
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate routing state: %w", err)
	}
	return result, nil
}

func (s *MongoDBStore) Upsert(ctx context.Context, entry Entry) error {
	entry, err := normalizeEntry(entry)
	if err != nil {
		return err
	}
	update := bson.M{
		"$set": bson.M{
			"kind":            string(entry.Kind),
			"provider_name":   entry.ProviderName,
			"canonical_model": entry.CanonicalModel,
			"model":           entry.Model,
			"enabled":         entry.Enabled,
			"reason":          entry.Reason,
			"updated_at":      entry.UpdatedAt,
		},
		"$setOnInsert": bson.M{
			"created_at": entry.CreatedAt,
		},
	}
	_, err = s.collection.UpdateOne(ctx, mongoEntryIDFilter{ID: entry.Key}, update, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("upsert routing state: %w", err)
	}
	return nil
}

func (s *MongoDBStore) Delete(ctx context.Context, key string) error {
	result, err := s.collection.DeleteOne(ctx, mongoEntryIDFilter{ID: strings.TrimSpace(key)})
	if err != nil {
		return fmt.Errorf("delete routing state: %w", err)
	}
	if result.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MongoDBStore) Close() error { return nil }

func entryFromMongo(doc mongoEntryDocument) Entry {
	return Entry{
		Key:            doc.ID,
		Kind:           Kind(doc.Kind),
		ProviderName:   doc.ProviderName,
		CanonicalModel: doc.CanonicalModel,
		Model:          doc.Model,
		Enabled:        doc.Enabled,
		Reason:         doc.Reason,
		CreatedAt:      doc.CreatedAt.UTC(),
		UpdatedAt:      doc.UpdatedAt.UTC(),
	}
}
