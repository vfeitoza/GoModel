package app

import (
	"log/slog"

	"gomodel/config"
	"gomodel/internal/intelligentrouter"
)

func newIntelligentRouterFromConfig(
	cfg config.IntelligentRoutingConfig,
	executor intelligentrouter.ChatCompletionExecutor,
	catalog intelligentrouter.Catalog,
	pricing intelligentrouter.PricingResolver,
	virtualResolver intelligentrouter.VirtualTargetResolver,
) (*intelligentrouter.Selector, error) {
	slog.Info("intelligent routing init", "enabled", cfg.Enabled, "mode", cfg.Mode, "analyzers", len(cfg.Analyzers))
	if !config.IntelligentRoutingActive(&cfg) {
		slog.Info("intelligent routing inactive, router will be nil")
		return nil, nil
	}

	analyzers := make([]intelligentrouter.AnalyzerConfig, 0, len(cfg.Analyzers))
	for _, analyzer := range cfg.Analyzers {
		maxTokens := analyzer.MaxTokens
		if maxTokens <= 0 {
			maxTokens = cfg.Defaults.MaxAnalysisTokens
		}
		analyzers = append(analyzers, intelligentrouter.AnalyzerConfig{
			Model:     analyzer.Model,
			Provider:  analyzer.Provider,
			MaxTokens: maxTokens,
		})
	}

	classifier, err := intelligentrouter.NewClassifier(
		executor,
		analyzers,
		cfg.Defaults.MaxAnalysisTokens,
		cfg.Defaults.Timeout,
		cfg.AnalysisUserPath,
	)
	if err != nil {
		return nil, err
	}

	selector := intelligentrouter.NewSelector(intelligentrouter.Config{
		Classifier:      classifier,
		Catalog:         catalog,
		Pricing:         pricing,
		VirtualResolver: virtualResolver,
		Filter: intelligentrouter.CandidateFilter{
			Allow: cfg.Candidates.Allow,
			Deny:  cfg.Candidates.Deny,
		},
		MinSavingsRatio: cfg.Defaults.MinSavingsRatio,
		MinConfidence:   cfg.Defaults.MinConfidence,
		FallbackModel:   cfg.FallbackModel,
		Mode:            cfg.Mode,
	})
	if selector == nil {
		slog.Warn("intelligent router selector is nil after construction (mode may be off)")
	} else {
		slog.Info("intelligent router ready", "mode", selector.Mode())
	}
	return selector, nil
}
