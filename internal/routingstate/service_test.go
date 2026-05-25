package routingstate

import (
	"context"
	"testing"

	"gomodel/internal/core"
	"gomodel/internal/routing"
)

type memoryStore struct{ entries map[string]Entry }

func (m *memoryStore) List(context.Context) ([]Entry, error) {
	result := make([]Entry, 0, len(m.entries))
	for _, entry := range m.entries {
		result = append(result, entry)
	}
	return result, nil
}
func (m *memoryStore) Upsert(_ context.Context, entry Entry) error { if m.entries == nil { m.entries = map[string]Entry{} }; m.entries[entry.Key] = entry; return nil }
func (m *memoryStore) Delete(_ context.Context, key string) error { delete(m.entries, key); return nil }
func (m *memoryStore) Close() error { return nil }

func TestServiceFilterCandidatesHonorsManualDisable(t *testing.T) {
	store := &memoryStore{}
	service, err := NewService(store)
	if err != nil { t.Fatalf("NewService() error = %v", err) }
	if err := service.Upsert(context.Background(), Entry{Kind: KindProvider, ProviderName: "anthropic_a", Enabled: false}); err != nil {
		t.Fatalf("Upsert provider state error = %v", err)
	}
	if err := service.Upsert(context.Background(), Entry{Kind: KindPoolCandidate, ProviderName: "anthropic_b", Model: "claude-sonnet-4-6", Enabled: false}); err != nil {
		t.Fatalf("Upsert candidate state error = %v", err)
	}
	candidates := []routing.Candidate{{Provider: "anthropic_a", Model: "claude-sonnet-4-6"}, {Provider: "anthropic_b", Model: "claude-sonnet-4-6"}, {Provider: "anthropic_c", Model: "claude-sonnet-4-6"}}
	filtered := service.FilterCandidates("claude-sonnet-4-6", candidates)
	if len(filtered) != 1 || filtered[0].Provider != "anthropic_c" {
		t.Fatalf("filtered = %+v, want only anthropic_c", filtered)
	}
}

func TestServiceCandidateEnabledDefaultsTrue(t *testing.T) {
	service, err := NewService(&memoryStore{})
	if err != nil { t.Fatalf("NewService() error = %v", err) }
	if !service.CandidateEnabled(core.ModelSelector{Provider: "anthropic_b", Model: "claude-opus-4-7"}) {
		t.Fatal("expected unspecified candidate to default to enabled")
	}
}
