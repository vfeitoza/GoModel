package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"gomodel/config"
	"gomodel/internal/providers"
	"gomodel/internal/routingstate"
)

func TestListRoutingModelPools_EmptyRegistry(t *testing.T) {
	h := NewHandler(nil, nil)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/routing/model-pools", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.ListRoutingModelPools(c); err != nil {
		t.Fatalf("ListRoutingModelPools() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestBuildRoutingPoolResponses_UsesRoutingConfigAndState(t *testing.T) {
	registry := providers.NewModelRegistry()
	service, err := routingstate.NewService(&routingStateMemoryStore{entries: map[string]routingstate.Entry{}})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.Upsert(context.Background(), routingstate.Entry{Kind: routingstate.KindProvider, ProviderName: "anthropic_a", Enabled: false}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := service.Upsert(context.Background(), routingstate.Entry{Kind: routingstate.KindCanonicalModel, CanonicalModel: "claude-sonnet-4-6", Enabled: false}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	h := NewHandler(nil, registry,
		WithRoutingState(service),
		WithRoutingConfig(config.RoutingConfig{
			Defaults: config.RoutingDefaultsConfig{Strategy: config.RoutingStrategyPriorityFailover},
			ModelPools: map[string]config.ModelPoolConfig{
				"claude-sonnet-4-6": {Candidates: []config.ModelPoolCandidateConfig{{Provider: "anthropic_a", Model: "claude-sonnet-4-6-20250929", Priority: 2, Weight: 8}, {Provider: "anthropic_b", Model: "claude-sonnet-4-6", Priority: 1, Weight: 10}}},
			},
		}),
	)
	responses := h.buildRoutingPoolResponses()
	if len(responses) != 1 {
		t.Fatalf("len(responses) = %d, want 1", len(responses))
	}
	if responses[0].Strategy != string(config.RoutingStrategyPriorityFailover) {
		t.Fatalf("Strategy = %q, want %q", responses[0].Strategy, config.RoutingStrategyPriorityFailover)
	}
	if responses[0].Enabled {
		t.Fatal("expected canonical model disabled")
	}
	if len(responses[0].Candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(responses[0].Candidates))
	}
	if responses[0].Candidates[0].Priority != 2 || responses[0].Candidates[0].Weight != 8 {
		t.Fatalf("first candidate priority/weight = %+v", responses[0].Candidates[0])
	}
	if responses[0].Candidates[0].Status != "disabled_manual" {
		t.Fatalf("first candidate status = %q, want disabled_manual", responses[0].Candidates[0].Status)
	}
	if responses[0].Candidates[0].IsConfigPrimary {
		t.Fatal("expected first candidate not to be config primary under priority_failover")
	}
	if !responses[0].Candidates[1].IsConfigPrimary {
		t.Fatal("expected second candidate to be config primary under priority_failover")
	}
	if !responses[0].Candidates[1].CandidateEnabled {
		t.Fatal("expected second candidate to remain directly enabled")
	}
	if responses[0].Candidates[1].Priority != 1 || responses[0].Candidates[1].Weight != 10 {
		t.Fatalf("second candidate priority/weight = %+v", responses[0].Candidates[1])
	}
	if responses[0].EffectiveCandidate != "" {
		t.Fatalf("EffectiveCandidate = %q, want empty when canonical model is disabled", responses[0].EffectiveCandidate)
	}
	if responses[0].ConfigPrimaryCandidate != "anthropic_b/claude-sonnet-4-6" {
		t.Fatalf("ConfigPrimaryCandidate = %q, want anthropic_b/claude-sonnet-4-6", responses[0].ConfigPrimaryCandidate)
	}
	if len(responses[0].BlockedCandidates) == 0 {
		t.Fatal("expected blocked candidates to be reported")
	}
}
