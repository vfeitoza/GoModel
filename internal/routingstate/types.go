package routingstate

import (
	"strings"
	"time"
)

type Kind string

const (
	KindProvider       Kind = "provider"
	KindCanonicalModel Kind = "canonical_model"
	KindPoolCandidate  Kind = "pool_candidate"
)

type Entry struct {
	Key            string    `json:"key" bson:"key"`
	Kind           Kind      `json:"kind" bson:"kind"`
	ProviderName   string    `json:"provider_name,omitempty" bson:"provider_name,omitempty"`
	CanonicalModel string    `json:"canonical_model,omitempty" bson:"canonical_model,omitempty"`
	Model          string    `json:"model,omitempty" bson:"model,omitempty"`
	Enabled        bool      `json:"enabled" bson:"enabled"`
	Reason         string    `json:"reason,omitempty" bson:"reason,omitempty"`
	CreatedAt      time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" bson:"updated_at"`
}

type View = Entry

func normalizeKind(kind Kind) Kind {
	return Kind(strings.ToLower(strings.TrimSpace(string(kind))))
}

func normalizeEntry(entry Entry) (Entry, error) {
	entry.Kind = normalizeKind(entry.Kind)
	entry.Key = strings.TrimSpace(entry.Key)
	entry.ProviderName = strings.TrimSpace(entry.ProviderName)
	entry.CanonicalModel = strings.TrimSpace(entry.CanonicalModel)
	entry.Model = strings.TrimSpace(entry.Model)
	entry.Reason = strings.TrimSpace(entry.Reason)

	switch entry.Kind {
	case KindProvider:
		if entry.ProviderName == "" {
			entry.ProviderName = entry.Key
		}
		if entry.ProviderName == "" {
			return Entry{}, newValidationError("provider_name is required", nil)
		}
		entry.Key = entry.ProviderName
	case KindCanonicalModel:
		if entry.CanonicalModel == "" {
			entry.CanonicalModel = entry.Key
		}
		if entry.CanonicalModel == "" {
			return Entry{}, newValidationError("canonical_model is required", nil)
		}
		entry.Key = entry.CanonicalModel
	case KindPoolCandidate:
		if entry.ProviderName == "" || entry.Model == "" {
			return Entry{}, newValidationError("provider_name and model are required for pool_candidate", nil)
		}
		if entry.Key == "" {
			entry.Key = entry.ProviderName + "/" + entry.Model
		}
	default:
		return Entry{}, newValidationError("kind must be one of: provider, canonical_model, pool_candidate", nil)
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	entry.UpdatedAt = time.Now().UTC()
	return entry, nil
}
