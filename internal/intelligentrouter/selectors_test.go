package intelligentrouter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsIntelligentSelector(t *testing.T) {
	tests := []struct {
		model        string
		wantOk       bool
		wantStrategy string
	}{
		{"auto", true, StrategyBalanced},
		{"AUTO", true, StrategyBalanced},
		{" smart ", true, StrategyBalanced},
		{"auto-cost", true, StrategyCost},
		{"auto-quality", true, StrategyQuality},
		{"gpt-4o", false, ""},
		{"", false, ""},
		{"claude-haiku-4-5", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			strategy, ok := IsIntelligentSelector(tt.model)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.Equal(t, tt.wantStrategy, strategy)
			}
		})
	}
}

func TestSelectorsAsModels_Defaults(t *testing.T) {
	models := SelectorsAsModels(DefaultSelectorNames)
	require.Len(t, models, 4)

	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
		require.Equal(t, "model", m.Object)
		require.Equal(t, "intelligent-router", m.OwnedBy)
		require.NotNil(t, m.Metadata)
		require.NotEmpty(t, m.Metadata.DisplayName)
		require.NotEmpty(t, m.Metadata.Description)
	}
	// Sorted alphabetically by ID.
	require.Equal(t, []string{"auto", "auto-cost", "auto-quality", "smart"}, ids)
}

func TestSelectorsAsModels_DropsEmptyAndDuplicates(t *testing.T) {
	models := SelectorsAsModels([]string{"auto", "", "  ", "auto", "custom-x"})
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	require.Equal(t, []string{"auto", "custom-x"}, ids)
}

func TestSelectorsAsModels_OperatorConfiguredFallbackDescription(t *testing.T) {
	models := SelectorsAsModels([]string{"my-router"})
	require.Len(t, models, 1)
	require.Equal(t, "my-router", models[0].ID)
	require.Contains(t, models[0].Metadata.Description, "configured by operator")
}
