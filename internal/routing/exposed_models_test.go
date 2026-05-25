package routing

import (
	"testing"
	"time"

	"gomodel/config"
	"gomodel/internal/core"
	"gomodel/internal/providers"
)

type testCatalog struct {
	models map[string]core.Model
}

func (c testCatalog) LookupModel(model string) (*core.Model, bool) {
	value, ok := c.models[model]
	if !ok {
		return nil, false
	}
	clone := value
	return &clone, true
}

type staticRuntimeProvider struct {
	snapshots []providers.ProviderRuntimeSnapshot
}

func (p staticRuntimeProvider) ProviderRuntimeSnapshots() []providers.ProviderRuntimeSnapshot {
	return append([]providers.ProviderRuntimeSnapshot(nil), p.snapshots...)
}

func TestCanonicalExposedModelLister_UsesEffectiveResolverChoice(t *testing.T) {
	now := time.Now().UTC()
	lister := NewCanonicalExposedModelLister(
		config.RoutingConfig{
			Defaults: config.RoutingDefaultsConfig{Strategy: config.RoutingStrategyWeightedRoundRobin},
			ModelPools: map[string]config.ModelPoolConfig{
				"claude-opus-4-7": {
					Candidates: []config.ModelPoolCandidateConfig{
						{Provider: "anthropic_a", Model: "claude-opus-4-7", Weight: 1, Priority: 2},
						{Provider: "anthropic_b", Model: "claude-opus-4-7", Weight: 10, Priority: 1},
					},
				},
			},
		},
		testCatalog{models: map[string]core.Model{
			"anthropic_a/claude-opus-4-7": {ID: "claude-opus-4-7", Object: "model", OwnedBy: "a"},
			"anthropic_b/claude-opus-4-7": {ID: "claude-opus-4-7", Object: "model", OwnedBy: "b"},
		}},
		nil,
		staticRuntimeProvider{snapshots: []providers.ProviderRuntimeSnapshot{
			{Name: "anthropic_a", Registered: true, DiscoveredModelCount: 1, LastModelFetchSuccessAt: &now},
			{Name: "anthropic_b", Registered: true, DiscoveredModelCount: 1, LastModelFetchSuccessAt: &now},
		}},
	)
	models := lister.ExposedModels()
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	if got := models[0].OwnedBy; got != "b" {
		t.Fatalf("models[0].OwnedBy = %q, want b", got)
	}
}

func TestCanonicalExposedModelLister_SkipsUnhealthyCandidates(t *testing.T) {
	now := time.Now().UTC()
	lister := NewCanonicalExposedModelLister(
		config.RoutingConfig{
			Defaults: config.RoutingDefaultsConfig{Strategy: config.RoutingStrategyPriorityFailover},
			ModelPools: map[string]config.ModelPoolConfig{
				"claude-sonnet-4-6": {
					Candidates: []config.ModelPoolCandidateConfig{
						{Provider: "anthropic_a", Model: "claude-sonnet-4-6-20250929", Priority: 1},
						{Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 2},
					},
				},
			},
		},
		testCatalog{models: map[string]core.Model{
			"anthropic_a/claude-sonnet-4-6-20250929": {ID: "claude-sonnet-4-6-20250929", Object: "model"},
			"anthropic_b/claude-sonnet-4-6":          {ID: "claude-sonnet-4-6", Object: "model"},
		}},
		nil,
		staticRuntimeProvider{snapshots: []providers.ProviderRuntimeSnapshot{
			{Name: "anthropic_a", Registered: true, LastModelFetchError: "boom"},
			{Name: "anthropic_b", Registered: true, DiscoveredModelCount: 1, LastModelFetchSuccessAt: &now},
		}},
	)

	models := lister.ExposedModels()
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	if got := models[0].ID; got != "claude-sonnet-4-6" {
		t.Fatalf("models[0].ID = %q, want claude-sonnet-4-6", got)
	}
}
