package intelligentrouter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gomodel/internal/core"
)

type fakeCatalog struct {
	models   []core.Model
	provider map[string]string
}

func (f fakeCatalog) ListModels() []core.Model { return f.models }
func (f fakeCatalog) Supports(model string) bool {
	for _, m := range f.models {
		if m.ID == model {
			return true
		}
	}
	return false
}
func (f fakeCatalog) GetProviderName(model string) string {
	if f.provider == nil {
		return ""
	}
	return f.provider[model]
}

func ptrInt(v int) *int { return &v }

func sampleModels() []core.Model {
	cheap := 0.2
	premium := 10.0
	standard := 2.0
	return []core.Model{
		{ID: "mini", Metadata: &core.ModelMetadata{
			Modes: []string{"chat"}, Pricing: &core.ModelPricing{InputPerMtok: &cheap, OutputPerMtok: &cheap},
			Capabilities: map[string]bool{"tools": true}, Tags: []string{"mini"},
		}},
		{ID: "pro", Metadata: &core.ModelMetadata{
			Modes: []string{"chat"}, Pricing: &core.ModelPricing{InputPerMtok: &standard, OutputPerMtok: &standard},
			Capabilities: map[string]bool{"tools": true, "code": true},
		}},
		{ID: "frontier", Metadata: &core.ModelMetadata{
			Modes: []string{"chat"}, Pricing: &core.ModelPricing{InputPerMtok: &premium, OutputPerMtok: &premium},
			Capabilities: map[string]bool{"reasoning": true, "vision": true}, Tags: []string{"premium"},
			ContextWindow: ptrInt(200000),
		}},
	}
}

func catalog() fakeCatalog {
	return fakeCatalog{
		models:   sampleModels(),
		provider: map[string]string{"mini": "openai", "pro": "openai", "frontier": "anthropic"},
	}
}

func TestBuildCandidates_AllowFilter(t *testing.T) {
	class := Classification{}
	cands := BuildCandidates(catalog(), CandidateFilter{Allow: []string{"openai/*"}}, nil, class, 0)
	require.Len(t, cands, 2) // mini, pro
	for _, c := range cands {
		require.Equal(t, "openai", c.Provider)
	}
}

func TestBuildCandidates_DenyWins(t *testing.T) {
	class := Classification{}
	cands := BuildCandidates(catalog(), CandidateFilter{Allow: []string{"*"}, Deny: []string{"frontier"}}, nil, class, 0)
	ids := candidateIDs(cands)
	require.ElementsMatch(t, []string{"mini", "pro"}, ids)
}

func TestBuildCandidates_VisionRequirement(t *testing.T) {
	class := Classification{RequiresVision: true}
	cands := BuildCandidates(catalog(), CandidateFilter{}, nil, class, 0)
	require.ElementsMatch(t, []string{"frontier"}, candidateIDs(cands))
}

func TestBuildCandidates_ToolsRequirement(t *testing.T) {
	class := Classification{RequiresTools: true}
	cands := BuildCandidates(catalog(), CandidateFilter{}, nil, class, 0)
	// Only mini and pro declare a tools capability; frontier is filtered out.
	require.ElementsMatch(t, []string{"mini", "pro"}, candidateIDs(cands))
}

func TestBuildCandidates_AllowOverrideReplacesAllow(t *testing.T) {
	class := Classification{}
	override := []string{"anthropic/*"}
	cands := BuildCandidates(catalog(), CandidateFilter{Allow: []string{"openai/*"}}, override, class, 0)
	require.ElementsMatch(t, []string{"frontier"}, candidateIDs(cands))
}

func candidateIDs(cands []Candidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.Selector.Model)
	}
	return out
}

func TestRankCandidates_CostStrategyPrefersCheap(t *testing.T) {
	cands := BuildCandidates(catalog(), CandidateFilter{}, nil, Classification{}, 0)
	ranked := RankCandidates(cands, nil, StrategyCost, Classification{})
	require.NotEmpty(t, ranked)
	require.Equal(t, "mini", ranked[0].Candidate.Selector.Model)
}

func TestRankCandidates_QualityStrategyPrefersPremium(t *testing.T) {
	cands := BuildCandidates(catalog(), CandidateFilter{}, nil, Classification{RequiresReasoning: true, QualitySensitivity: "high"}, 0)
	ranked := RankCandidates(cands, nil, StrategyQuality, Classification{RequiresReasoning: true, QualitySensitivity: "high"})
	require.NotEmpty(t, ranked)
	require.Equal(t, "frontier", ranked[0].Candidate.Selector.Model)
}

// noPricingCatalog returns models with no pricing configured — the realistic
// case for self-hosted gateways whose model registry is not enriched.
func noPricingCatalog() fakeCatalog {
	return fakeCatalog{
		models: []core.Model{
			{ID: "mini", Metadata: &core.ModelMetadata{Modes: []string{"chat"}, Capabilities: map[string]bool{"code": false}}},
			{ID: "pro", Metadata: &core.ModelMetadata{Modes: []string{"chat"}, Capabilities: map[string]bool{"code": true}}},
			{ID: "frontier", Metadata: &core.ModelMetadata{Modes: []string{"chat"}, Capabilities: map[string]bool{"reasoning": true}}},
		},
		provider: map[string]string{"mini": "openai", "pro": "openai", "frontier": "anthropic"},
	}
}

// TestRankCandidates_CostAbstainsWhenNoPricingKnown verifies Item 1: when every
// candidate has the same (unknown) cost, the cost dimension abstains and the
// decision is driven by capability/quality instead. With a coding request under
// cost strategy, abstention means the code-capable model wins, not the cheapest
// (there is no cheapest — all costs are identical).
func TestRankCandidates_CostAbstainsWhenNoPricingKnown(t *testing.T) {
	cands := BuildCandidates(noPricingCatalog(), CandidateFilter{}, nil, Classification{RequiresCode: true}, 0)
	require.Len(t, cands, 3)

	ranked := RankCandidates(cands, nil, StrategyCost, Classification{RequiresCode: true})
	require.NotEmpty(t, ranked)
	// No price signal → cost abstains → capability decides → the code model wins.
	require.Equal(t, "pro", ranked[0].Candidate.Selector.Model)
}

// TestRankCandidates_FreeModelWinsOnCostDimension verifies Item 2: a model
// tagged "free" beats paid models on the cost dimension, and paid models are
// capped proportionally so the gap stays visible.
func TestRankCandidates_FreeModelWinsOnCostDimension(t *testing.T) {
	paid := 1.0
	free := 0.0
	cat := fakeCatalog{
		models: []core.Model{
			{ID: "free-local", Metadata: &core.ModelMetadata{
				Modes: []string{"chat"}, Tags: []string{"free", "local"},
				Pricing: &core.ModelPricing{InputPerMtok: &free, OutputPerMtok: &free},
			}},
			{ID: "cheap-paid", Metadata: &core.ModelMetadata{
				Modes: []string{"chat"}, Tags: []string{"mini"},
				Pricing: &core.ModelPricing{InputPerMtok: &paid, OutputPerMtok: &paid},
			}},
		},
		provider: map[string]string{"free-local": "ollama", "cheap-paid": "openai"},
	}

	cands := BuildCandidates(cat, CandidateFilter{}, nil, Classification{}, 0)
	ranked := RankCandidates(cands, nil, StrategyCost, Classification{})
	require.NotEmpty(t, ranked)
	require.Equal(t, "free-local", ranked[0].Candidate.Selector.Model)
}

// TestBuildCandidates_ContextPenaltyInRiskZone verifies Item 3: a request that
// sits in the risk zone (>80% of a model's window) receives a proportional
// context penalty rather than being excluded.
func TestBuildCandidates_ContextPenaltyInRiskZone(t *testing.T) {
	// Model with a 32k window. A 100k-char request estimates ~25k tokens → ~78%
	// usage, just under the threshold → no penalty. A 110k-char request → ~27.5k
	// tokens → ~86% usage → in the risk zone.
	cat := fakeCatalog{
		models: []core.Model{
			{ID: "big", Metadata: &core.ModelMetadata{Modes: []string{"chat"}, ContextWindow: ptrInt(32000)}},
			{ID: "huge", Metadata: &core.ModelMetadata{Modes: []string{"chat"}, ContextWindow: ptrInt(200000)}},
		},
		provider: map[string]string{"big": "openai", "huge": "anthropic"},
	}

	comfortable := BuildCandidates(cat, CandidateFilter{}, nil, Classification{}, 100000)
	bigC := findCandidate(comfortable, "big")
	require.NotNil(t, bigC)
	require.InDelta(t, 1.0, bigC.ContextScore, 0.001, "78%% usage should be a comfortable fit")

	inRisk := BuildCandidates(cat, CandidateFilter{}, nil, Classification{}, 110000)
	bigR := findCandidate(inRisk, "big")
	require.NotNil(t, bigR)
	require.Less(t, bigR.ContextScore, 1.0, "86%% usage should trigger the risk penalty")
	require.Greater(t, bigR.ContextScore, 0.0, "risk zone is penalized, not excluded")

	hugeR := findCandidate(inRisk, "huge")
	require.NotNil(t, hugeR)
	require.InDelta(t, 1.0, hugeR.ContextScore, 0.001, "large-window model stays at full score")
}

// TestBuildCandidates_ContextHardExcludeWhenOverLimit verifies Item 3: a request
// whose estimated token count exceeds a model's window is hard-excluded.
func TestBuildCandidates_ContextHardExcludeWhenOverLimit(t *testing.T) {
	cat := fakeCatalog{
		models: []core.Model{
			{ID: "small", Metadata: &core.ModelMetadata{Modes: []string{"chat"}, ContextWindow: ptrInt(8000)}},
			{ID: "big", Metadata: &core.ModelMetadata{Modes: []string{"chat"}, ContextWindow: ptrInt(200000)}},
		},
		provider: map[string]string{"small": "openai", "big": "anthropic"},
	}

	// 200k chars → ~50k tokens. "small" (8k) cannot fit → excluded; "big" fits.
	cands := BuildCandidates(cat, CandidateFilter{}, nil, Classification{}, 200000)
	ids := candidateIDs(cands)
	require.NotContains(t, ids, "small")
	require.Contains(t, ids, "big")
}

// TestBuildCandidates_ContextNoFilterWhenWindowUnknown verifies Item 3: a model
// without a declared context window is never penalized, even for huge requests.
func TestBuildCandidates_ContextNoFilterWhenWindowUnknown(t *testing.T) {
	cat := fakeCatalog{
		models: []core.Model{
			{ID: "unknown", Metadata: &core.ModelMetadata{Modes: []string{"chat"}}},
		},
		provider: map[string]string{"unknown": "openai"},
	}

	cands := BuildCandidates(cat, CandidateFilter{}, nil, Classification{}, 10_000_000)
	require.Len(t, cands, 1)
	require.InDelta(t, 1.0, cands[0].ContextScore, 0.001)
}

func findCandidate(cands []Candidate, model string) *Candidate {
	for i := range cands {
		if cands[i].Selector.Model == model {
			return &cands[i]
		}
	}
	return nil
}
