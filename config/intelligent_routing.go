package config

import (
	"fmt"
	"strings"
	"time"
)

// IntelligentRoutingConfig holds configuration for the optional intelligent
// model router. When disabled (the default), GoModel executes exactly the
// model the client requested. When enabled, a configured analyzer model
// classifies the request and the gateway selects a better-fitting candidate
// from the catalog before execution.
//
// See docs/dev/intelligent-model.md for the full design and rollout phases.
type IntelligentRoutingConfig struct {
	// Enabled controls whether the intelligent router is constructed at all.
	// Even when enabled, only intelligent selectors (auto/smart/auto-cost/
	// auto-quality) or intelligent virtual models trigger analysis unless Mode
	// is observe, in which case every request may be classified for metrics.
	// Default: false
	Enabled bool `yaml:"enabled" env:"INTELLIGENT_ROUTING_ENABLED"`

	// Mode selects how decisions are applied:
	//   - off:     no analysis; intelligent selectors behave as unknown models
	//   - observe: classify and record the recommendation, but still execute
	//              the originally requested model (dry-run)
	//   - enforce: classify and route to the selected model for intelligent
	//              selectors
	// Default: "off"
	Mode string `yaml:"mode" env:"INTELLIGENT_ROUTING_MODE"`

	// Analyzers is the ordered pool of cheap models used to classify the
	// request. They are attempted in order; on failure or timeout the next
	// analyzer is tried. At least one analyzer is required when enabled.
	Analyzers []AnalyzerModelConfig `yaml:"analyzers"`

	// Defaults holds resolved defaults shared by every selector.
	Defaults IntelligentDefaults `yaml:"defaults"`

	// Selectors maps intelligent selector names to selection strategies.
	// Names not listed here still resolve to Defaults.Strategy.
	Selectors []IntelligentSelectorConfig `yaml:"selectors"`

	// Candidates constrains which catalog models are eligible for selection.
	Candidates CandidateFilterConfig `yaml:"candidates"`

	// FallbackModel is the selector used when analysis fails entirely (all
	// analyzers errored or timed out). Empty falls back to the selector's
	// configured default, or to a model_not_found error when none is set.
	FallbackModel string `yaml:"fallback_model" env:"INTELLIGENT_ROUTING_FALLBACK_MODEL"`

	// AnalysisUserPath scopes the analyzer call's usage/audit records so the
	// cost of analysis is reported separately from the main execution.
	// Default: "/intelligent-router"
	AnalysisUserPath string `yaml:"analysis_user_path" env:"INTELLIGENT_ROUTING_ANALYSIS_USER_PATH"`
}

// AnalyzerModelConfig describes one analyzer in the pool.
type AnalyzerModelConfig struct {
	// Model is the model selector used for the classification call. This can
	// be a concrete model name, a provider-qualified selector, or an alias.
	Model string `yaml:"model"`

	// Provider is an optional routing hint for Model.
	Provider string `yaml:"provider"`

	// MaxTokens limits the analyzer completion. Overrides Defaults.MaxAnalysisTokens
	// when non-zero.
	MaxTokens int `yaml:"max_tokens"`
}

// IntelligentDefaults holds shared default values for intelligent routing.
type IntelligentDefaults struct {
	// Strategy is the default selection strategy: cost, balanced, quality, or latency.
	// Default: "balanced"
	Strategy string `yaml:"strategy" env:"INTELLIGENT_ROUTING_DEFAULT_STRATEGY"`

	// MaxAnalysisTokens limits the analyzer completion. Default: 256
	MaxAnalysisTokens int `yaml:"max_analysis_tokens" env:"INTELLIGENT_ROUTING_MAX_ANALYSIS_TOKENS"`

	// Timeout bounds a single analyzer call, as a Go duration string
	// (e.g. "1500ms", "2s"). Default: 1500ms
	Timeout time.Duration `yaml:"timeout" env:"INTELLIGENT_ROUTING_TIMEOUT"`

	// MinSavingsRatio is the minimum estimated savings ratio required to
	// switch to a cheaper model in enforce mode. Default: 0.15
	MinSavingsRatio float64 `yaml:"min_savings_ratio" env:"INTELLIGENT_ROUTING_MIN_SAVINGS_RATIO"`

	// MinConfidence is the minimum classifier confidence to switch to a
	// cheaper model; below it a stronger model is chosen. Default: 0.7
	MinConfidence float64 `yaml:"min_confidence" env:"INTELLIGENT_ROUTING_MIN_CONFIDENCE"`
}

// IntelligentSelectorConfig maps an intelligent selector name to a strategy.
type IntelligentSelectorConfig struct {
	// Name is the selector clients send, e.g. "auto", "auto-cost".
	Name string `yaml:"name"`

	// Strategy overrides Defaults.Strategy for this selector.
	Strategy string `yaml:"strategy"`

	// DefaultModel is used when analysis fails and no global FallbackModel is set.
	DefaultModel string `yaml:"default_model"`
}

// CandidateFilterConfig restricts eligible catalog models.
// When Allow is non-empty, only matching models are eligible. Deny always wins.
type CandidateFilterConfig struct {
	Allow []string `yaml:"allow" env:"INTELLIGENT_ROUTING_CANDIDATES_ALLOW"`
	Deny  []string `yaml:"deny"  env:"INTELLIGENT_ROUTING_CANDIDATES_DENY"`
}

// DefaultIntelligentRoutingConfig returns the disabled default configuration.
func DefaultIntelligentRoutingConfig() IntelligentRoutingConfig {
	return IntelligentRoutingConfig{
		Mode:             IntelligentRoutingModeOff,
		AnalysisUserPath: "/intelligent-router",
		Defaults: IntelligentDefaults{
			Strategy:          IntelligentStrategyBalanced,
			MaxAnalysisTokens: 256,
			Timeout:           1500 * time.Millisecond,
			MinSavingsRatio:   0.15,
			MinConfidence:     0.7,
		},
	}
}

// Intelligent routing mode values.
const (
	IntelligentRoutingModeOff     = "off"
	IntelligentRoutingModeObserve = "observe"
	IntelligentRoutingModeEnforce = "enforce"
)

// Selection strategy values.
const (
	IntelligentStrategyCost     = "cost"
	IntelligentStrategyBalanced = "balanced"
	IntelligentStrategyQuality  = "quality"
	IntelligentStrategyLatency  = "latency"
)

// IntelligentRoutingModeValid reports whether mode is a recognized value.
func IntelligentRoutingModeValid(mode string) bool {
	switch mode {
	case IntelligentRoutingModeOff, IntelligentRoutingModeObserve, IntelligentRoutingModeEnforce:
		return true
	}
	return false
}

// IntelligentStrategyValid reports whether strategy is a recognized value.
func IntelligentStrategyValid(strategy string) bool {
	switch strategy {
	case IntelligentStrategyCost, IntelligentStrategyBalanced, IntelligentStrategyQuality, IntelligentStrategyLatency:
		return true
	}
	return false
}

// ValidateIntelligentRoutingConfig validates and normalizes the intelligent
// routing configuration. It is a no-op when the feature is disabled.
func ValidateIntelligentRoutingConfig(cfg *IntelligentRoutingConfig) error {
	if cfg == nil {
		return nil
	}
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = IntelligentRoutingModeOff
	}
	if !IntelligentRoutingModeValid(cfg.Mode) {
		return fmt.Errorf("intelligent_routing.mode: must be one of off, observe, enforce; got %q", cfg.Mode)
	}

	applyIntelligentRoutingDefaults(&cfg.Defaults)
	if !IntelligentStrategyValid(cfg.Defaults.Strategy) {
		return fmt.Errorf("intelligent_routing.defaults.strategy: must be one of cost, balanced, quality, latency; got %q", cfg.Defaults.Strategy)
	}
	if cfg.Defaults.MaxAnalysisTokens <= 0 {
		return fmt.Errorf("intelligent_routing.defaults.max_analysis_tokens: must be greater than 0")
	}
	if cfg.Defaults.Timeout <= 0 {
		return fmt.Errorf("intelligent_routing.defaults.timeout: must be greater than 0")
	}
	if cfg.Defaults.MinSavingsRatio < 0 || cfg.Defaults.MinSavingsRatio > 1 {
		return fmt.Errorf("intelligent_routing.defaults.min_savings_ratio: must be between 0 and 1")
	}
	if cfg.Defaults.MinConfidence < 0 || cfg.Defaults.MinConfidence > 1 {
		return fmt.Errorf("intelligent_routing.defaults.min_confidence: must be between 0 and 1")
	}

	cfg.AnalysisUserPath = strings.TrimSpace(cfg.AnalysisUserPath)
	cfg.FallbackModel = strings.TrimSpace(cfg.FallbackModel)

	for i := range cfg.Analyzers {
		cfg.Analyzers[i].Model = strings.TrimSpace(cfg.Analyzers[i].Model)
		cfg.Analyzers[i].Provider = strings.TrimSpace(cfg.Analyzers[i].Provider)
	}

	seen := make(map[string]struct{}, len(cfg.Selectors))
	for i := range cfg.Selectors {
		name := strings.ToLower(strings.TrimSpace(cfg.Selectors[i].Name))
		if name == "" {
			return fmt.Errorf("intelligent_routing.selectors[%d].name is required", i)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("intelligent_routing.selectors[%d].name duplicate %q", i, name)
		}
		seen[name] = struct{}{}
		cfg.Selectors[i].Name = name
		strategy := strings.ToLower(strings.TrimSpace(cfg.Selectors[i].Strategy))
		if strategy != "" && !IntelligentStrategyValid(strategy) {
			return fmt.Errorf("intelligent_routing.selectors[%d].strategy: must be one of cost, balanced, quality, latency; got %q", i, strategy)
		}
		cfg.Selectors[i].Strategy = strategy
		cfg.Selectors[i].DefaultModel = strings.TrimSpace(cfg.Selectors[i].DefaultModel)
	}

	// Keep the feature disabled unless explicitly enabled.
	if !cfg.Enabled {
		return nil
	}

	if len(cfg.Analyzers) == 0 {
		return fmt.Errorf("intelligent_routing.analyzers: at least one analyzer is required when intelligent routing is enabled")
	}
	for i, a := range cfg.Analyzers {
		if a.Model == "" {
			return fmt.Errorf("intelligent_routing.analyzers[%d].model is required", i)
		}
	}

	return nil
}

// applyIntelligentRoutingDefaults fills zero-value defaults.
func applyIntelligentRoutingDefaults(d *IntelligentDefaults) {
	if strings.TrimSpace(d.Strategy) == "" {
		d.Strategy = IntelligentStrategyBalanced
	}
	if d.MaxAnalysisTokens == 0 {
		d.MaxAnalysisTokens = 256
	}
	if d.Timeout == 0 {
		d.Timeout = 1500 * time.Millisecond
	}
	if d.MinSavingsRatio == 0 {
		d.MinSavingsRatio = 0.15
	}
	if d.MinConfidence == 0 {
		d.MinConfidence = 0.7
	}
}

// IntelligentRoutingActive reports whether the feature is enabled and not off.
func IntelligentRoutingActive(cfg *IntelligentRoutingConfig) bool {
	if cfg == nil || !cfg.Enabled {
		return false
	}
	return cfg.Mode == IntelligentRoutingModeObserve || cfg.Mode == IntelligentRoutingModeEnforce
}
