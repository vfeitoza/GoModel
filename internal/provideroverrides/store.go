package provideroverrides

import (
	"context"
)

// Store defines the interface for persisting provider overrides.
type Store interface {
	// List returns all provider overrides.
	List(ctx context.Context) ([]ProviderOverride, error)
	// Upsert creates or updates a provider override.
	Upsert(ctx context.Context, override ProviderOverride) error
	// Delete removes a provider override by name.
	Delete(ctx context.Context, providerName string) error
	// Close releases resources held by the store.
	Close(ctx context.Context) error
}

// Catalog defines the interface for looking up provider information.
type Catalog interface {
	// ProviderNames returns all registered provider names.
	ProviderNames() []string
	// ProviderExists returns true if a provider with the given name exists.
	ProviderExists(providerName string) bool
}