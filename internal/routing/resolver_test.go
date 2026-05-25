package routing

import (
	"context"
	"testing"
	"time"

	"gomodel/config"
	"gomodel/internal/core"
	"gomodel/internal/providers"
)

func TestNewResolverPriorityFailover(t *testing.T) {
	resolver := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{
			Strategy:           config.RoutingStrategyPriorityFailover,
			SessionAffinity:    true,
			SessionAffinityTTL: 30 * time.Minute,
		},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-sonnet-4-6": {
				Candidates: []config.ModelPoolCandidateConfig{
					{Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 1},
					{Provider: "anthropic_a", Model: "claude-sonnet-4-6-20250929", Priority: 2},
				},
			},
		},
	})
	if resolver == nil {
		t.Fatal("NewResolver() = nil, want resolver")
	}

	resolution, matched, err := resolver.Resolve(core.NewRequestedModelSelector("claude-sonnet-4-6", ""))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !matched {
		t.Fatal("matched = false, want true")
	}
	if got := resolution.Primary.QualifiedModel(); got != "anthropic_b/claude-sonnet-4-6" {
		t.Fatalf("primary = %q, want anthropic_b/claude-sonnet-4-6", got)
	}
	if len(resolution.Fallbacks) != 1 || resolution.Fallbacks[0].QualifiedModel() != "anthropic_a/claude-sonnet-4-6-20250929" {
		t.Fatalf("fallbacks = %v, want [anthropic_a/claude-sonnet-4-6-20250929]", resolution.Fallbacks)
	}
}

func TestNewResolverWeightedRoundRobinDistributesPrimary(t *testing.T) {
	resolver := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{
			Strategy: config.RoutingStrategyWeightedRoundRobin,
		},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-opus-4-7": {
				Candidates: []config.ModelPoolCandidateConfig{
					{Provider: "anthropic_b", Model: "claude-opus-4-7", Weight: 10, Priority: 1},
					{Provider: "anthropic_a", Model: "claude-opus-4-7", Weight: 8, Priority: 2},
				},
			},
		},
	})

	seen := map[string]int{}
	for i := 0; i < 4; i++ {
		resolution, matched, err := resolver.Resolve(core.NewRequestedModelSelector("claude-opus-4-7", ""))
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if !matched {
			t.Fatal("matched = false, want true")
		}
		seen[resolution.Primary.QualifiedModel()]++
	}
	if len(seen) < 2 {
		t.Fatalf("weighted selection saw %v, want at least 2 candidates chosen", seen)
	}
}

func TestResolverReturnsErrorWhenCanonicalModelDisabled(t *testing.T) {
	resolver := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{Strategy: config.RoutingStrategyPriorityFailover},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-sonnet-4-6": {Candidates: []config.ModelPoolCandidateConfig{{Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 1}}},
		},
	}, disabledCanonicalStateChecker{})
	_, matched, err := resolver.Resolve(core.NewRequestedModelSelector("claude-sonnet-4-6", ""))
	if err == nil {
		t.Fatal("expected error for disabled canonical model")
	}
	if matched {
		t.Fatal("matched = true, want false on disabled canonical model")
	}
}

func TestResolverSkipsUnhealthyPrimaryCandidate(t *testing.T) {
	resolver := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{Strategy: config.RoutingStrategyPriorityFailover},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-sonnet-4-6": {
				Candidates: []config.ModelPoolCandidateConfig{
					{Provider: "anthropic_a", Model: "claude-sonnet-4-6-20250929", Priority: 1},
					{Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 2},
				},
			},
		},
	}).WithRuntime(staticRuntimeProvider{snapshots: []providers.ProviderRuntimeSnapshot{{Name: "anthropic_a", Registered: true, LastModelFetchError: "boom"}, {Name: "anthropic_b", Registered: true, DiscoveredModelCount: 1}}})
	resolution, matched, err := resolver.Resolve(core.NewRequestedModelSelector("claude-sonnet-4-6", ""))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !matched {
		t.Fatal("matched = false, want true")
	}
	if got := resolution.Primary.QualifiedModel(); got != "anthropic_b/claude-sonnet-4-6" {
		t.Fatalf("primary = %q, want anthropic_b/claude-sonnet-4-6", got)
	}
}

func TestResolverKeepsDegradedCandidateEligible(t *testing.T) {
	resolver := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{Strategy: config.RoutingStrategyPriorityFailover},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-sonnet-4-6": {
				Candidates: []config.ModelPoolCandidateConfig{
					{Provider: "anthropic_a", Model: "claude-sonnet-4-6-20250929", Priority: 1},
					{Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 2},
				},
			},
		},
	}).WithRuntime(staticRuntimeProvider{snapshots: []providers.ProviderRuntimeSnapshot{{Name: "anthropic_a", Registered: true, DiscoveredModelCount: 1, LastModelFetchError: "temporary issue"}, {Name: "anthropic_b", Registered: true, DiscoveredModelCount: 1}}})
	resolution, matched, err := resolver.Resolve(core.NewRequestedModelSelector("claude-sonnet-4-6", ""))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !matched {
		t.Fatal("matched = false, want true")
	}
	if got := resolution.Primary.QualifiedModel(); got != "anthropic_a/claude-sonnet-4-6-20250929" {
		t.Fatalf("primary = %q, want degraded candidate to remain eligible", got)
	}
}

func TestResolverUsesSessionAffinityForSameUserPath(t *testing.T) {
	resolver := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{
			Strategy:        config.RoutingStrategyWeightedRoundRobin,
			SessionAffinity: true,
		},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-opus-4-7": {
				Candidates: []config.ModelPoolCandidateConfig{
					{Provider: "anthropic_a", Model: "claude-opus-4-7", Weight: 10, Priority: 1},
					{Provider: "anthropic_b", Model: "claude-opus-4-7", Weight: 10, Priority: 2},
				},
			},
		},
	})
	ctx := core.WithRequestID(context.Background(), "req-1")
	ctx = core.WithRequestSnapshot(ctx, (&core.RequestSnapshot{UserPath: "/team/a"}))
	first, matched, err := resolver.ResolveWithContext(ctx, core.NewRequestedModelSelector("claude-opus-4-7", ""))
	if err != nil {
		t.Fatalf("first ResolveWithContext() error = %v", err)
	}
	if !matched {
		t.Fatal("first matched = false, want true")
	}
	second, matched, err := resolver.ResolveWithContext(ctx, core.NewRequestedModelSelector("claude-opus-4-7", ""))
	if err != nil {
		t.Fatalf("second ResolveWithContext() error = %v", err)
	}
	if !matched {
		t.Fatal("second matched = false, want true")
	}
	if first.Primary.QualifiedModel() != second.Primary.QualifiedModel() {
		t.Fatalf("affinity primary mismatch: first=%q second=%q", first.Primary.QualifiedModel(), second.Primary.QualifiedModel())
	}
}

func TestResolverRepinsWhenPinnedCandidateBecomesIneligible(t *testing.T) {
	resolver := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{
			Strategy:        config.RoutingStrategyPriorityFailover,
			SessionAffinity: true,
		},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-sonnet-4-6": {
				Candidates: []config.ModelPoolCandidateConfig{
					{Provider: "anthropic_a", Model: "claude-sonnet-4-6-20250929", Priority: 1},
					{Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 2},
				},
			},
		},
	})
	ctx := core.WithRequestID(context.Background(), "req-1")
	ctx = core.WithRequestSnapshot(ctx, (&core.RequestSnapshot{UserPath: "/team/a"}))
	first, matched, err := resolver.ResolveWithContext(ctx, core.NewRequestedModelSelector("claude-sonnet-4-6", ""))
	if err != nil {
		t.Fatalf("first ResolveWithContext() error = %v", err)
	}
	if !matched {
		t.Fatal("first matched = false, want true")
	}
	resolver.runtime = staticRuntimeProvider{snapshots: []providers.ProviderRuntimeSnapshot{{Name: "anthropic_a", Registered: true, LastModelFetchError: "boom"}, {Name: "anthropic_b", Registered: true, DiscoveredModelCount: 1}}}
	second, matched, err := resolver.ResolveWithContext(ctx, core.NewRequestedModelSelector("claude-sonnet-4-6", ""))
	if err != nil {
		t.Fatalf("second ResolveWithContext() error = %v", err)
	}
	if !matched {
		t.Fatal("second matched = false, want true")
	}
	if first.Primary.QualifiedModel() == second.Primary.QualifiedModel() {
		t.Fatalf("expected repin after runtime failure, both resolutions used %q", second.Primary.QualifiedModel())
	}
}

func TestResolverRepinsWhenPinnedCandidateIsManuallyDisabled(t *testing.T) {
	state := &manualDisableStateChecker{}
	resolver := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{
			Strategy:        config.RoutingStrategyPriorityFailover,
			SessionAffinity: true,
		},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-sonnet-4-6": {
				Candidates: []config.ModelPoolCandidateConfig{
					{Provider: "anthropic_a", Model: "claude-sonnet-4-6-20250929", Priority: 1},
					{Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 2},
				},
			},
		},
	}, state)
	ctx := core.WithRequestID(context.Background(), "req-1")
	ctx = core.WithRequestSnapshot(ctx, (&core.RequestSnapshot{UserPath: "/team/a"}))
	first, matched, err := resolver.ResolveWithContext(ctx, core.NewRequestedModelSelector("claude-sonnet-4-6", ""))
	if err != nil {
		t.Fatalf("first ResolveWithContext() error = %v", err)
	}
	if !matched {
		t.Fatal("first matched = false, want true")
	}
	state.disabledProvider = "anthropic_a"
	state.disabledModel = "claude-sonnet-4-6-20250929"
	second, matched, err := resolver.ResolveWithContext(ctx, core.NewRequestedModelSelector("claude-sonnet-4-6", ""))
	if err != nil {
		t.Fatalf("second ResolveWithContext() error = %v", err)
	}
	if !matched {
		t.Fatal("second matched = false, want true")
	}
	if first.Primary.QualifiedModel() == second.Primary.QualifiedModel() {
		t.Fatalf("expected repin after manual disable, both resolutions used %q", second.Primary.QualifiedModel())
	}
}

func TestComposedResolverAppliesAliasThenPool(t *testing.T) {
	alias := aliasResolverStub{}
	pool := NewResolver(config.RoutingConfig{
		Defaults: config.RoutingDefaultsConfig{Strategy: config.RoutingStrategyPriorityFailover},
		ModelPools: map[string]config.ModelPoolConfig{
			"claude-sonnet-4-6": {
				Candidates: []config.ModelPoolCandidateConfig{{Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 1}},
			},
		},
	})
	resolver := NewComposedResolver(alias, pool)

	selector, changed, err := resolver.ResolveModel(core.NewRequestedModelSelector("friendly-sonnet", ""))
	if err != nil {
		t.Fatalf("ResolveModel() error = %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if got := selector.QualifiedModel(); got != "anthropic_b/claude-sonnet-4-6" {
		t.Fatalf("selector = %q, want anthropic_b/claude-sonnet-4-6", got)
	}
}

type disabledCanonicalStateChecker struct{}

func (disabledCanonicalStateChecker) CanonicalModelEnabled(name string) bool {
	return name != "claude-sonnet-4-6"
}

func (disabledCanonicalStateChecker) FilterCandidates(_ string, candidates []Candidate) []Candidate {
	return candidates
}

type manualDisableStateChecker struct {
	disabledProvider string
	disabledModel    string
}

func (s *manualDisableStateChecker) CanonicalModelEnabled(string) bool {
	return true
}

func (s *manualDisableStateChecker) FilterCandidates(_ string, candidates []Candidate) []Candidate {
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Provider == s.disabledProvider && candidate.Model == s.disabledModel {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func (s *manualDisableStateChecker) ProviderEnabled(string) bool {
	return true
}

func (s *manualDisableStateChecker) CandidateEnabled(selector core.ModelSelector) bool {
	return selector.Provider != s.disabledProvider || selector.Model != s.disabledModel
}

type aliasResolverStub struct{}

func (aliasResolverStub) ResolveModel(requested core.RequestedModelSelector) (core.ModelSelector, bool, error) {
	if requested.RequestedQualifiedModel() == "friendly-sonnet" {
		return core.ModelSelector{Model: "claude-sonnet-4-6"}, true, nil
	}
	selector, err := requested.Normalize()
	return selector, false, err
}
