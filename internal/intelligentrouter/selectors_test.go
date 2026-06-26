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
