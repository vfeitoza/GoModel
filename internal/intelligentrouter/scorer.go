package intelligentrouter

import (
	"sort"
	"strings"
	"time"

	"gomodel/internal/core"
)

// ScoreCandidate is a candidate paired with its computed score.
type ScoreCandidate struct {
	Candidate   Candidate
	Score       float64
	UnitCost    float64 // estimated per-1M-token blended cost, for reporting
	HealthScore float64 // 1.0 = healthy, 0.0 = circuit breaker tripped
}

// HealthConfig parameterises the health dimension of the scorer.
type HealthConfig struct {
	Window         time.Duration
	HalfLife       time.Duration
	PseudoCounts   float64
	CircuitBreaker float64
}

// defaultHealthConfig is used when RankCandidates is called without explicit health settings.
var defaultHealthConfig = HealthConfig{
	Window:         healthDefaultWindow,
	HalfLife:       healthDefaultHalfLife,
	PseudoCounts:   healthDefaultPseudoCounts,
	CircuitBreaker: healthDefaultCircuitBreaker,
}

// RankCandidates scores and sorts candidates for the given strategy and
// classification. Higher score is better. The cost dimension is computed across
// all candidates at once so it can abstain (contribute nothing) when no
// candidate has a discriminating price signal, and so a free model can cap the
// paid ones proportionally instead of treating zero cost as unknown.
//
// Models whose circuit breaker has tripped (HealthScore == 0) are excluded from
// the ranked list before any scoring occurs.
func RankCandidates(candidates []Candidate, pricing PricingResolver, strategy string, class Classification) []ScoreCandidate {
	return RankCandidatesWithHealth(candidates, pricing, strategy, class, defaultHealthConfig)
}

// RankCandidatesWithHealth is the full entry point used by the Selector.
func RankCandidatesWithHealth(candidates []Candidate, pricing PricingResolver, strategy string, class Classification, healthCfg HealthConfig) []ScoreCandidate {
	strategy = normalizeStrategy(strategy)

	costScores := computeCostScores(candidates, pricing)

	now := time.Now()
	var scored []ScoreCandidate
	for i, c := range candidates {
		qualifiedID := c.Selector.QualifiedModel()
		healthScore := ModelHealthScore(qualifiedID, now, healthCfg.Window, healthCfg.HalfLife, healthCfg.PseudoCounts, healthCfg.CircuitBreaker)
		if healthScore <= 0 {
			// Circuit breaker tripped — hard-exclude this candidate.
			continue
		}
		sc := ScoreCandidate{
			Candidate:   c,
			Score:       scoreCandidate(c.Model, costScores[i], c.ContextScore, strategy, class),
			UnitCost:    estimateUnitCost(c.Model, pricing),
			HealthScore: healthScore,
		}
		// Apply health penalty: blend keeps some raw score for degraded but
		// non-tripped models, so a temporarily-struggling model isn't silently
		// pushed to last place by a tie-break alone.
		sc.Score *= healthFitFactor(healthScore)
		scored = append(scored, sc)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		// Tie-break: healthier first, then cheaper.
		if scored[i].HealthScore != scored[j].HealthScore {
			return scored[i].HealthScore > scored[j].HealthScore
		}
		return scored[i].UnitCost < scored[j].UnitCost
	})
	return scored
}

// healthFitFactor maps a health score to a raw-score multiplier.
// A score of 1.0 leaves the result unchanged; a near-zero score applies a
// floor of 0.7 so that a struggling (but not broken) model is penalized
// without being collapsed to near-zero when other signals are strong.
func healthFitFactor(healthScore float64) float64 {
	if healthScore <= 0 {
		return 0
	}
	if healthScore >= 1 {
		return 1
	}
	return 0.7 + 0.3*healthScore
}

func normalizeStrategy(strategy string) string {
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	switch strategy {
	case StrategyCost, StrategyBalanced, StrategyQuality, StrategyLatency:
		return strategy
	default:
		return StrategyBalanced
	}
}

// scoreCandidate returns a non-negative score; higher is better. The shape is a
// simple, explicit weighting per strategy so it stays auditable and testable.
//
// costScore is computed by the caller (RankCandidates) so the cost dimension can
// abstain (0.0 for every candidate) when no candidate has a discriminating price
// signal, and so a free model can cap the paid ones proportionally.
//
// contextScore (0.0–1.0) scales the final result down for models whose context
// window is near its limit. A score of 1.0 leaves the result unchanged; lower
// values apply a proportional penalty so a tight-fit model ranks below a model
// with comfortable headroom, without excluding it entirely.
func scoreCandidate(model *core.Model, costScore, contextScore float64, strategy string, class Classification) float64 {
	tier := modelTier(model)
	quality := tierQualityScore(tier) // free=1, cheap=1, standard=2, premium=3

	var raw float64
	switch strategy {
	case StrategyCost:
		raw = 10*costScore + 0.1*float64(quality) + capabilityBonus(model, class)
	case StrategyQuality:
		base := float64(quality)
		if class.RequiresReasoning || class.QualitySensitivity == "high" {
			base *= 1.5
		}
		raw = base + capabilityBonus(model, class)
	case StrategyLatency:
		// Prefer cheaper tiers as a latency proxy and small-context models.
		raw = 5*costScore + float64(4-quality) + capabilityBonus(model, class)
	default: // balanced
		raw = 4*costScore + float64(quality) + capabilityBonus(model, class)
	}

	// Apply the context-fit penalty on top of the raw score. When contextScore is
	// 1.0 (comfortable or unknown) this is a no-op.
	return raw * contextFitFactor(contextScore)
}

// contextFitFactor maps a context fit score to a multiplier on the raw score.
// It blends rather than replaces so a strong capability match can still surface a
// model that sits in the risk zone. A 0.10 score (near the limit) keeps ~70% of
// the raw score; a 1.0 score keeps it all.
func contextFitFactor(contextScore float64) float64 {
	if contextScore <= 0 {
		return 0
	}
	if contextScore >= 1 {
		return 1
	}
	// Blend: floor the multiplier at 0.7 so a tight fit is penalized, not
	// annihilated, when other signals are strong.
	return 0.7 + 0.3*contextScore
}

// capabilityBonus rewards models that match the classification's hard signals,
// keeping "balanced" from always picking the cheapest option.
func capabilityBonus(model *core.Model, class Classification) float64 {
	var bonus float64
	if class.RequiresCode && capabilityPtr(model, "code") {
		bonus += 1
	}
	if class.RequiresReasoning && capabilityPtr(model, "reasoning") {
		bonus += 1
	}
	return bonus
}

func capabilityPtr(model *core.Model, key string) bool {
	if model == nil || model.Metadata == nil || model.Metadata.Capabilities == nil {
		return false
	}
	return model.Metadata.Capabilities[key]
}

// tierQualityScore maps a derived tier to an ordinal quality weight.
func tierQualityScore(tier string) int {
	switch tier {
	case "premium":
		return 3
	case "standard":
		return 2
	default: // free, cheap, or unknown
		return 1
	}
}

// modelTier derives a coarse price tier from metadata tags. Tags are the
// operator's explicit signal; "free"/"local"/"self-hosted" mark a free model
// (local Ollama/vLLM) so it can win the cost dimension without competing on
// price with paid models.
func modelTier(model *core.Model) string {
	if model != nil && model.Metadata != nil {
		for _, tag := range model.Metadata.Tags {
			switch strings.ToLower(strings.TrimSpace(tag)) {
			case "premium", "frontier":
				return "premium"
			case "free", "local", "self-hosted":
				return "free"
			case "cheap", "mini", "flash", "haiku", "lite":
				return "cheap"
			}
		}
	}
	return "standard"
}

// computeCostScores returns the cost dimension score (0.0–1.0, higher is better)
// for each candidate, in order. The score is computed across all candidates at
// once so the dimension can abstain and a free model can cap the paid ones:
//
//   - Abstention: when every candidate has the same estimated cost (no
//     discriminating price signal), the cost dimension contributes nothing
//     (all scores 0.0). This prevents neutral noise from diluting quality and
//     capability signals in balanced/cost strategies.
//   - Free model: a candidate tagged "free"/"local"/"self-hosted" (or with a
//     zero blended cost alongside paid candidates) receives 1.0; paid
//     candidates are then capped at 0.5 so the free model always wins on cost
//     while paid models keep a proportional, visible gap among themselves.
//   - Paid-only: the cheapest paid candidate receives 1.0 and the rest scale as
//     minCost / theirCost (2x as expensive = 0.5, 10x = 0.1).
func computeCostScores(candidates []Candidate, pricing PricingResolver) []float64 {
	scores := make([]float64, len(candidates))
	if len(candidates) == 0 {
		return scores
	}

	tiers := make([]string, len(candidates))
	costs := make([]float64, len(candidates))
	hasFree := false
	for i, c := range candidates {
		costs[i] = estimateUnitCost(c.Model, pricing)
		tiers[i] = modelTier(c.Model)
		if tiers[i] == "free" {
			hasFree = true
		}
	}

	// Abstention: identical (or all-unknown) cost across the pool → no signal.
	minCost, maxCost := costs[0], costs[0]
	for _, c := range costs[1:] {
		if c < minCost {
			minCost = c
		}
		if c > maxCost {
			maxCost = c
		}
	}
	if maxCost-minCost < costSpreadThreshold {
		// No discriminating signal on the cost axis. Leave all at 0.0 so the
		// dimension does not dilute quality/capability in the weighted sum.
		return scores
	}

	paidCosts := make([]float64, 0, len(candidates))
	for i, c := range costs {
		if tiers[i] == "free" || c == 0 {
			continue
		}
		paidCosts = append(paidCosts, c)
	}
	hasFree = hasFree || (len(paidCosts) < len(candidates))
	minPaid := 0.0
	if len(paidCosts) > 0 {
		minPaid = paidCosts[0]
		for _, c := range paidCosts[1:] {
			if c < minPaid {
				minPaid = c
			}
		}
	}

	paidCeiling := 1.0
	if hasFree {
		paidCeiling = 0.5
	}

	for i, tier := range tiers {
		switch {
		case tier == "free":
			scores[i] = 1.0
		case costs[i] <= 0:
			// Unknown cost without a free tag: treat as neutral-mid so it does
			// not dominate a paid field but is not excluded either.
			scores[i] = paidCeiling / 2
		default:
			if minPaid > 0 {
				scores[i] = paidCeiling * (minPaid / costs[i])
			} else {
				scores[i] = paidCeiling
			}
		}
	}
	return scores
}

const costSpreadThreshold = 0.0001

// estimateUnitCost blends input+output per-1M-token pricing into a single
// comparable USD figure. Returns 0 when pricing is unavailable.
func estimateUnitCost(model *core.Model, pricing PricingResolver) float64 {
	if model == nil {
		return 0
	}
	var in, out float64
	if model.Metadata != nil && model.Metadata.Pricing != nil {
		in = floatPtrValue(model.Metadata.Pricing.InputPerMtok)
		out = floatPtrValue(model.Metadata.Pricing.OutputPerMtok)
	}
	// Prefer effective pricing from the resolver when available (reflects overrides).
	if pricing != nil {
		providerType := ""
		if model.Metadata != nil {
			providerType = strings.TrimSpace(model.OwnedBy)
		}
		if p := pricing.ResolvePricing(model.ID, providerType); p != nil {
			if p.InputPerMtok != nil {
				in = *p.InputPerMtok
			}
			if p.OutputPerMtok != nil {
				out = *p.OutputPerMtok
			}
		}
	}
	if in <= 0 && out <= 0 {
		return 0
	}
	return in + out
}

func floatPtrValue(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
