package intelligentrouter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gomodel/internal/core"
	"gomodel/internal/observability"
)

// Config configures the Selector.
type Config struct {
	Classifier      *Classifier
	Catalog         Catalog
	Pricing         PricingResolver
	VirtualResolver VirtualTargetResolver
	Filter          CandidateFilter
	MinSavingsRatio float64
	MinConfidence   float64
	FallbackModel   string // selector used when analysis fails
	Mode            string
	// Selectors are the intelligent selector names and their strategies to
	// recognise at request time. When empty, the built-in defaults (auto, smart,
	// auto-cost, auto-quality) are used.
	Selectors []SelectorConfig
}

// SelectorConfig pairs a selector name with the strategy it should apply.
type SelectorConfig struct {
	Name     string
	Strategy string
}

// Selector classifies a request and selects the best catalog model.
type Selector struct {
	classifier         *Classifier
	catalog            Catalog
	pricing            PricingResolver
	virtual            VirtualTargetResolver
	filter             CandidateFilter
	minSavings         float64
	minConf            float64
	fallback           string
	mode               string
	selectorStrategies map[string]string
}

// NewSelector constructs a Selector. Returns nil (no error) when the feature is
// not active, so the caller can store a nil router and treat it as disabled.
func NewSelector(cfg Config) *Selector {
	if cfg.Classifier == nil || cfg.Catalog == nil {
		return nil
	}
	mode := normalizeMode(cfg.Mode)
	if mode == ModeOff {
		return nil
	}
	minSavings := cfg.MinSavingsRatio
	if minSavings <= 0 {
		minSavings = 0.15
	}
	minConf := cfg.MinConfidence
	if minConf <= 0 {
		minConf = 0.7
	}

	strategies := make(map[string]string, len(cfg.Selectors))
	if len(cfg.Selectors) == 0 {
		for _, name := range DefaultSelectorNames {
			if strat, ok := defaultSelectorStrategy[name]; ok {
				strategies[name] = strat
			}
		}
	} else {
		for _, sel := range cfg.Selectors {
			name := strings.ToLower(strings.TrimSpace(sel.Name))
			if name == "" {
				continue
			}
			strat := strings.ToLower(strings.TrimSpace(sel.Strategy))
			if strat == "" {
				strat = defaultSelectorStrategy[name]
			}
			strategies[name] = strat
		}
	}

	return &Selector{
		classifier:         cfg.Classifier,
		catalog:            cfg.Catalog,
		pricing:            cfg.Pricing,
		virtual:            cfg.VirtualResolver,
		filter:             cfg.Filter,
		minSavings:         minSavings,
		minConf:            minConf,
		fallback:           cfg.FallbackModel,
		mode:               mode,
		selectorStrategies: strategies,
	}
}

// ExposedModels returns the intelligent selectors projected as virtual model
// entries for inclusion in GET /v1/models. Returns nil when the router is nil.
func (s *Selector) ExposedModels() []core.Model {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.selectorStrategies))
	for name := range s.selectorStrategies {
		names = append(names, name)
	}
	return SelectorsAsModels(names)
}

func normalizeMode(mode string) string {
	switch mode {
	case ModeObserve, ModeEnforce:
		return mode
	default:
		return ModeOff
	}
}

// Mode returns the active routing mode.
func (s *Selector) Mode() string { return s.mode }

// RecordExecution records the outcome of a provider call so the health tracker
// can penalize or exclude unhealthy models in future routing decisions.
func (s *Selector) RecordExecution(qualifiedModel string, success bool) {
	RecordHealth(qualifiedModel, success)
}

// IsSelector reports whether name is a configured intelligent selector.
func (s *Selector) IsSelector(name string) bool {
	if s == nil {
		return false
	}
	_, isConfigured := s.selectorStrategies[name]
	return isConfigured
}

// ShouldEvaluate reports whether the requested selector should trigger
// intelligent routing. It returns the strategy to use and whether the request
// is an intelligent virtual model (whose targets override the candidate filter).
func (s *Selector) ShouldEvaluate(requested core.RequestedModelSelector, meta SelectionMeta) (strategy string, ok bool) {
	// Check configured selector strategies
	if strat, isConfigured := s.selectorStrategies[requested.Model]; isConfigured {
		return resolveStrategy(strat, meta), true
	}
	// Intelligent virtual model?
	if s.virtual != nil && !requested.ExplicitProvider {
		if _, vmStrategy, isVM := s.virtual.IntelligentTargets(requested.Model, meta.UserPath); isVM {
			return resolveStrategy(vmStrategy, meta), true
		}
	}
	return "", false
}

func resolveStrategy(base string, meta SelectionMeta) string {
	if meta.Strategy != "" {
		return meta.Strategy
	}
	return base
}

// Evaluate runs classification + scoring and returns a Decision. It does not
// mutate the request; the caller applies SelectedModel when in enforce mode.
func (s *Selector) Evaluate(ctx context.Context, req *core.ChatRequest, requested core.RequestedModelSelector, meta SelectionMeta) *Decision {
	start := time.Now()
	decision := &Decision{
		Requested: requested,
		Mode:      s.mode,
		Strategy:  meta.Strategy,
	}

	allowOverride := meta.CandidateAllow
	if s.virtual != nil && !requested.ExplicitProvider {
		if targets, vmStrategy, ok := s.virtual.IntelligentTargets(requested.Model, meta.UserPath); ok {
			allowOverride = selectorsToPatterns(targets)
			if meta.Strategy == "" && vmStrategy != "" {
				decision.Strategy = vmStrategy
			}
		}
	}

	// Build a first-pass candidate list before classification so the analyzer can
	// see operator-provided routing guidance for the eligible pool. The final
	// candidate list is rebuilt after classification to apply capability filters
	// (vision/tools/long-context) derived from the analyzer output.
	analysisCandidates := BuildCandidates(s.catalog, s.filter, allowOverride, Classification{}, estimateRequestChars(req))
	history := getRoutingHistory(meta.UserPath, meta.ConversationID, routingMemoryDefaultN)

	class, analyzerUsed, err := s.classifier.ClassifyWithCandidates(ctx, req, analysisCandidates, history)
	if err != nil {
		decision.AnalysisFailed = true
		decision.Analyzers = s.classifier.Analyzers()
		decision.Duration = time.Since(start)
		decision.SelectedModel = s.fallbackSelector(requested, meta)
		decision.Reason = "analysis failed: " + err.Error()
		// In enforce, still apply the fallback so analysis failure never blocks the
		// user's request; in observe, execute the requested model unchanged.
		decision.applyForMode(s.mode, requested)
		logDecision(decision)
		return decision
	}

	decision.Classification = &class
	decision.Analyzers = s.classifier.Analyzers()
	decision.AnalyzerUsed = analyzerUsed
	decision.Confidence = class.Confidence
	decision.Strategy = resolveStrategy(classToStrategy(class, meta.Strategy), meta)

	// Recompute the allowlist if the intelligent virtual model supplied an
	// explicit strategy and the analyzer did not override it.
	if s.virtual != nil && !requested.ExplicitProvider {
		if targets, vmStrategy, ok := s.virtual.IntelligentTargets(requested.Model, meta.UserPath); ok {
			allowOverride = selectorsToPatterns(targets)
			if meta.Strategy == "" && vmStrategy != "" {
				decision.Strategy = vmStrategy
			}
		}
	}
	candidates := BuildCandidates(s.catalog, s.filter, allowOverride, class, estimateRequestChars(req))
	scored := RankCandidates(candidates, s.pricing, decision.Strategy, class)
	decision.SelectedModel = s.choose(scored, requested, class)
	decision.Duration = time.Since(start)
	decision.Reason = buildReason(class, scored, decision.SelectedModel)
	decision.applyForMode(s.mode, requested)
	if decision.Applied && decision.AppliedModel.Model != "" && strings.TrimSpace(meta.ConversationID) != "" {
		addRoutingDecision(meta.UserPath, meta.ConversationID, decision.AppliedModel.QualifiedModel())
	}
	logDecision(decision)
	return decision
}

// applyForMode sets AppliedModel/Applied according to the routing mode.
func (d *Decision) applyForMode(mode string, requested core.RequestedModelSelector) {
	switch mode {
	case ModeEnforce:
		d.AppliedModel = d.SelectedModel
		d.Applied = true
	default: // observe
		// Keep the requested model as the one actually executed, but preserve the
		// recommendation in SelectedModel for metrics/audit.
		d.AppliedModel = requestedSelector(requested)
		d.Applied = false
	}
}

func (s *Selector) choose(scored []ScoreCandidate, requested core.RequestedModelSelector, class Classification) core.ModelSelector {
	if len(scored) == 0 {
		return s.fallbackSelector(requested, SelectionMeta{})
	}
	// Low confidence: prefer a stronger (higher quality) candidate.
	if class.Confidence < s.minConf && len(scored) > 1 {
		// Pick the highest-quality candidate rather than the top score.
		best := scored[0]
		for _, c := range scored[1:] {
			if tierQualityScore(modelTier(c.Candidate.Model)) >
				tierQualityScore(modelTier(best.Candidate.Model)) {
				best = c
			}
		}
		return best.Candidate.Selector
	}
	return scored[0].Candidate.Selector
}

func (s *Selector) fallbackSelector(requested core.RequestedModelSelector, meta SelectionMeta) core.ModelSelector {
	if fb := parseFallback(s.fallback); fb.Model != "" {
		return fb
	}
	return requestedSelector(requested)
}

func parseFallback(s string) core.ModelSelector {
	s = normalizeSelectorStr(s)
	if s == "" {
		return core.ModelSelector{}
	}
	selector, err := core.ParseModelSelector(s, "")
	if err != nil {
		return core.ModelSelector{}
	}
	return selector
}

func requestedSelector(requested core.RequestedModelSelector) core.ModelSelector {
	selector, err := requested.Normalize()
	if err != nil {
		return core.ModelSelector{Model: requested.Model, Provider: requested.ProviderHint}
	}
	return selector
}

func selectorsToPatterns(selectors []core.ModelSelector) []string {
	patterns := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		if selector.Model == "" {
			continue
		}
		patterns = append(patterns, selector.QualifiedModel())
	}
	return patterns
}

// estimateRequestChars sums the visible text length of every message so the
// catalog can score how comfortably each model's context window fits the
// request. Attachments, images, and audio are ignored — only text contributes.
func estimateRequestChars(req *core.ChatRequest) int {
	if req == nil {
		return 0
	}
	total := 0
	for _, msg := range req.Messages {
		total += len(core.ExtractTextContent(msg.Content))
	}
	return total
}

func classToStrategy(class Classification, metaStrategy string) string {
	if metaStrategy != "" {
		return metaStrategy
	}
	if class.SuggestedTier == "premium" || class.QualitySensitivity == "high" || class.RequiresReasoning {
		return StrategyQuality
	}
	if class.SuggestedTier == "cheap" {
		return StrategyCost
	}
	return StrategyBalanced
}

func buildReason(class Classification, scored []ScoreCandidate, selected core.ModelSelector) string {
	if len(scored) == 0 {
		return fmt.Sprintf("no candidates; complexity=%s task=%s tier=%s", class.Complexity, class.TaskType, class.SuggestedTier)
	}
	return fmt.Sprintf("complexity=%s task=%s tier=%s confidence=%.2f -> %s", class.Complexity, class.TaskType, class.SuggestedTier, class.Confidence, selected.QualifiedModel())
}

func normalizeSelectorStr(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// ErrNoCandidates is returned when no catalog model is eligible.
var ErrNoCandidates = errors.New("intelligent router: no eligible candidate models")

func logDecision(d *Decision) {
	if d == nil {
		return
	}
	applied := strconv.FormatBool(d.Applied)
	failed := strconv.FormatBool(d.AnalysisFailed)
	observability.IntelligentRoutingRequestsTotal.WithLabelValues(d.Mode, d.Strategy, applied, failed).Inc()
	observability.IntelligentRoutingDecisionLatency.WithLabelValues(d.Mode, d.Strategy, failed).Observe(d.Duration.Seconds())
	if d.AnalysisFailed {
		observability.IntelligentRoutingFallbacksTotal.WithLabelValues(d.Mode, d.Strategy).Inc()
	}
	if d.Confidence > 0 && d.Confidence < 0.7 {
		observability.IntelligentRoutingLowConfidenceTotal.WithLabelValues(d.Mode, d.Strategy).Inc()
	}
	slog.Info("intelligent routing decision",
		"requested", d.Requested.RequestedQualifiedModel(),
		"selected", d.SelectedModel.QualifiedModel(),
		"applied", d.Applied,
		"applied_model", d.AppliedModel.QualifiedModel(),
		"analyzer", d.AnalyzerUsed.QualifiedModel(),
		"strategy", d.Strategy,
		"mode", d.Mode,
		"confidence", d.Confidence,
		"analysis_failed", d.AnalysisFailed,
		"duration_ms", d.Duration.Milliseconds(),
		"reason", d.Reason,
	)
}
