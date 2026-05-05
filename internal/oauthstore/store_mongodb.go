package oauthstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoOAuthTokenDocument struct {
	ProviderName     string    `bson:"_id"`
	ProviderType     string    `bson:"provider_type"`
	AccessToken      string    `bson:"access_token"`
	RefreshToken     string    `bson:"refresh_token"`
	ExpiresAt        time.Time `bson:"expires_at"`
	Scopes           string    `bson:"scopes"`
	AccountEmail     string    `bson:"account_email"`
	AccountID        string    `bson:"account_id"`
	DisplayName      string    `bson:"display_name"`
	SubscriptionType string    `bson:"subscription_type"`
	CreatedAt        time.Time `bson:"created_at"`
	UpdatedAt        time.Time `bson:"updated_at"`
}

// MongoDBStore stores OAuth tokens in MongoDB.
type MongoDBStore struct {
	collection *mongo.Collection
}

// NewMongoDBStore creates collection indexes if needed.
func NewMongoDBStore(database *mongo.Database) (*MongoDBStore, error) {
	if database == nil {
		return nil, fmt.Errorf("database is required")
	}
	coll := database.Collection("oauth_tokens")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "expires_at", Value: 1}}},
		{Keys: bson.D{{Key: "provider_type", Value: 1}}},
	}
	if _, err := coll.Indexes().CreateMany(ctx, indexes); err != nil {
		return nil, fmt.Errorf("create oauth_tokens indexes: %w", err)
	}
	return &MongoDBStore{collection: coll}, nil
}

func (s *MongoDBStore) Save(ctx context.Context, token *Token) error {
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

	doc := mongoOAuthTokenDocument{
		ProviderName:     name,
		ProviderType:     strings.TrimSpace(token.ProviderType),
		AccessToken:      token.AccessToken,
		RefreshToken:     token.RefreshToken,
		ExpiresAt:        token.ExpiresAt.UTC(),
		Scopes:           joinScopes(token.Scopes),
		AccountEmail:     strings.TrimSpace(token.AccountEmail),
		AccountID:        strings.TrimSpace(token.AccountID),
		DisplayName:      strings.TrimSpace(token.DisplayName),
		SubscriptionType: strings.TrimSpace(token.SubscriptionType),
		CreatedAt:        createdAt,
		UpdatedAt:        now,
	}

	upsert := true
	_, err = s.collection.ReplaceOne(
		ctx,
		bson.M{"_id": name},
		doc,
		options.Replace().SetUpsert(upsert),
	)
	if err != nil {
		return fmt.Errorf("save oauth token: %w", err)
	}
	return nil
}

func (s *MongoDBStore) Get(ctx context.Context, providerName string) (*Token, error) {
	name := normalizeProviderName(providerName)
	if name == "" {
		return nil, fmt.Errorf("provider_name is required")
	}

	var doc mongoOAuthTokenDocument
	err := s.collection.FindOne(ctx, bson.M{"_id": name}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get oauth token: %w", err)
	}
	return tokenFromMongo(doc), nil
}

func (s *MongoDBStore) Delete(ctx context.Context, providerName string) error {
	name := normalizeProviderName(providerName)
	if name == "" {
		return fmt.Errorf("provider_name is required")
	}
	_, err := s.collection.DeleteOne(ctx, bson.M{"_id": name})
	if err != nil {
		return fmt.Errorf("delete oauth token: %w", err)
	}
	return nil
}

func (s *MongoDBStore) List(ctx context.Context) ([]*Token, error) {
	cursor, err := s.collection.Find(
		ctx,
		bson.M{},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}),
	)
	if err != nil {
		return nil, fmt.Errorf("list oauth tokens: %w", err)
	}
	defer cursor.Close(ctx)

	result := make([]*Token, 0)
	for cursor.Next(ctx) {
		var doc mongoOAuthTokenDocument
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode oauth token: %w", err)
		}
		result = append(result, tokenFromMongo(doc))
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate oauth tokens: %w", err)
	}
	return result, nil
}

func (s *MongoDBStore) Close() error {
	return nil
}

func tokenFromMongo(doc mongoOAuthTokenDocument) *Token {
	return &Token{
		ProviderName:     doc.ProviderName,
		ProviderType:     doc.ProviderType,
		AccessToken:      doc.AccessToken,
		RefreshToken:     doc.RefreshToken,
		ExpiresAt:        doc.ExpiresAt.UTC(),
		Scopes:           splitScopes(doc.Scopes),
		AccountEmail:     doc.AccountEmail,
		AccountID:        doc.AccountID,
		DisplayName:      doc.DisplayName,
		SubscriptionType: doc.SubscriptionType,
		CreatedAt:        doc.CreatedAt.UTC(),
		UpdatedAt:        doc.UpdatedAt.UTC(),
	}
}
