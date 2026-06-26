package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultIntelligentRoutingConfig(t *testing.T) {
	cfg := DefaultIntelligentRoutingConfig()
	require.False(t, cfg.Enabled)
	require.Equal(t, IntelligentRoutingModeOff, cfg.Mode)
	require.Equal(t, IntelligentStrategyBalanced, cfg.Defaults.Strategy)
	require.Equal(t, 256, cfg.Defaults.MaxAnalysisTokens)
	require.Equal(t, 1500*time.Millisecond, cfg.Defaults.Timeout)
	require.Equal(t, 0.15, cfg.Defaults.MinSavingsRatio)
	require.Equal(t, 0.7, cfg.Defaults.MinConfidence)
	require.Equal(t, "/intelligent-router", cfg.AnalysisUserPath)
	require.False(t, IntelligentRoutingActive(&cfg))
}

func TestValidateIntelligentRoutingConfig_DisabledNoOp(t *testing.T) {
	// Disabled config requires nothing and is accepted as-is.
	cfg := &IntelligentRoutingConfig{Enabled: false}
	require.NoError(t, ValidateIntelligentRoutingConfig(cfg))
	require.Equal(t, IntelligentRoutingModeOff, cfg.Mode)
	require.Equal(t, IntelligentStrategyBalanced, cfg.Defaults.Strategy)
}

func TestValidateIntelligentRoutingConfig_InvalidMode(t *testing.T) {
	cfg := &IntelligentRoutingConfig{Enabled: true, Mode: "always"}
	err := ValidateIntelligentRoutingConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mode")
}

func TestValidateIntelligentRoutingConfig_InvalidStrategy(t *testing.T) {
	cfg := &IntelligentRoutingConfig{Enabled: true, Mode: IntelligentRoutingModeEnforce}
	cfg.Analyzers = []AnalyzerModelConfig{{Model: "gpt-5.4-mini"}}
	cfg.Defaults.Strategy = "cheapest"
	err := ValidateIntelligentRoutingConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "strategy")
}

func TestValidateIntelligentRoutingConfig_EnabledRequiresAnalyzer(t *testing.T) {
	cfg := &IntelligentRoutingConfig{Enabled: true, Mode: IntelligentRoutingModeObserve}
	err := ValidateIntelligentRoutingConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one analyzer")
}

func TestValidateIntelligentRoutingConfig_EnabledValid(t *testing.T) {
	cfg := &IntelligentRoutingConfig{
		Enabled: true,
		Mode:    IntelligentRoutingModeEnforce,
		Analyzers: []AnalyzerModelConfig{
			{Model: "gpt-5.4-mini", Provider: "codex"},
			{Model: "glm-5-turbo", Provider: "zai"},
			{Model: "claude-haiku-4-5", Provider: "anthropic"},
		},
	}
	require.NoError(t, ValidateIntelligentRoutingConfig(cfg))
	require.True(t, IntelligentRoutingActive(cfg))
}

func TestValidateIntelligentRoutingConfig_Selectors(t *testing.T) {
	t.Run("valid selectors normalized", func(t *testing.T) {
		cfg := &IntelligentRoutingConfig{
			Enabled:   true,
			Mode:      IntelligentRoutingModeEnforce,
			Analyzers: []AnalyzerModelConfig{{Model: "gpt-5.4-mini"}},
			Selectors: []IntelligentSelectorConfig{
				{Name: "AUTO", Strategy: "COST"},
				{Name: "auto-quality", Strategy: "quality"},
			},
		}
		require.NoError(t, ValidateIntelligentRoutingConfig(cfg))
		require.Equal(t, "auto", cfg.Selectors[0].Name)
		require.Equal(t, "cost", cfg.Selectors[0].Strategy)
	})
	t.Run("duplicate selector rejected", func(t *testing.T) {
		cfg := &IntelligentRoutingConfig{
			Enabled:   true,
			Mode:      IntelligentRoutingModeEnforce,
			Analyzers: []AnalyzerModelConfig{{Model: "gpt-5.4-mini"}},
			Selectors: []IntelligentSelectorConfig{
				{Name: "auto"},
				{Name: "AUTO"},
			},
		}
		err := ValidateIntelligentRoutingConfig(cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate")
	})
}

func TestValidateIntelligentRoutingConfig_RatioBounds(t *testing.T) {
	for _, ratio := range []float64{-0.1, 1.5} {
		cfg := &IntelligentRoutingConfig{
			Enabled:   true,
			Mode:      IntelligentRoutingModeObserve,
			Analyzers: []AnalyzerModelConfig{{Model: "gpt-5.4-mini"}},
		}
		cfg.Defaults.MinSavingsRatio = ratio
		err := ValidateIntelligentRoutingConfig(cfg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "min_savings_ratio")
	}
}

func TestApplyEnvOverrides_IntelligentRouting(t *testing.T) {
	t.Setenv("INTELLIGENT_ROUTING_ENABLED", "true")
	t.Setenv("INTELLIGENT_ROUTING_MODE", "enforce")
	t.Setenv("INTELLIGENT_ROUTING_DEFAULT_STRATEGY", "cost")
	t.Setenv("INTELLIGENT_ROUTING_TIMEOUT", "2s")
	t.Setenv("INTELLIGENT_ROUTING_MAX_ANALYSIS_TOKENS", "512")

	cfg := buildDefaultConfig()
	require.NoError(t, applyEnvOverrides(cfg))

	require.True(t, cfg.IntelligentRouting.Enabled)
	require.Equal(t, IntelligentRoutingModeEnforce, cfg.IntelligentRouting.Mode)
	require.Equal(t, IntelligentStrategyCost, cfg.IntelligentRouting.Defaults.Strategy)
	require.Equal(t, 2*time.Second, cfg.IntelligentRouting.Defaults.Timeout)
	require.Equal(t, 512, cfg.IntelligentRouting.Defaults.MaxAnalysisTokens)
}
