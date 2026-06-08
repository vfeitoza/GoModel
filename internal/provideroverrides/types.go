package provideroverrides

import (
	"sort"
	"strings"
	"time"
)

// DefaultEnabledProviders is the default enabled state for providers.
const DefaultEnabledProviders = true

// ProviderOverride represents an override for a provider's enabled state.
type ProviderOverride struct {
	// ProviderName is the name of the provider this override applies to.
	ProviderName string `json:"provider_name" db:"provider_name"`
	// Enabled indicates whether the provider is enabled.
	Enabled bool `json:"enabled" db:"enabled"`
	// CreatedAt is when this override was created.
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	// UpdatedAt is when this override was last updated.
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// Clone returns a deep copy of this override.
func (o ProviderOverride) Clone() ProviderOverride {
	return ProviderOverride{
		ProviderName: o.ProviderName,
		Enabled:      o.Enabled,
		CreatedAt:    o.CreatedAt,
		UpdatedAt:    o.UpdatedAt,
	}
}

// View is the API response view for a provider override.
type View struct {
	ProviderName string    `json:"provider_name"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    string    `json:"created_at"`
	UpdatedAt    string    `json:"updated_at"`
}

// NewView creates a View from a ProviderOverride.
func NewView(o ProviderOverride) View {
	return View{
		ProviderName: o.ProviderName,
		Enabled:      o.Enabled,
		CreatedAt:    formatTimestamp(o.CreatedAt),
		UpdatedAt:    formatTimestamp(o.UpdatedAt),
	}
}

func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// normalizeProviderName normalizes a provider name for comparison.
func normalizeProviderName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

// normalizeStoredOverride normalizes a stored override for consistency.
func normalizeStoredOverride(o ProviderOverride) ProviderOverride {
	o.ProviderName = normalizeProviderName(o.ProviderName)
	return o
}

// sortOverrides sorts overrides by provider name.
func sortOverrides(overrides []ProviderOverride) {
	sort.Slice(overrides, func(i, j int) bool {
		return overrides[i].ProviderName < overrides[j].ProviderName
	})
}