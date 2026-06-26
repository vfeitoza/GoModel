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
