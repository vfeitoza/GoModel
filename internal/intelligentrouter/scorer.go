package intelligentrouter

import (
	"sort"
	"strings"

	"gomodel/internal/core"
)

// ScoreCandidate is a candidate paired with its computed score.
type ScoreCandidate struct {
	Candidate Candidate
	Score     float64
	UnitCost  float64 // estimated per-1M-token blended cost, for reporting
}

// RankCandidates scores and sorts candidates for the given strategy and
// classification. Higher score is better. Candidates without usable pricing are
// kept but ranked below priced ones within cost-sensitive strategies.
func RankCandidates(candidates []Candidate, pricing PricingResolver, strategy string, class Classification) []ScoreCandidate {
	strategy = normalizeStrategy(strategy)
	scored := make([]ScoreCandidate, 0, len(candidates))
	for _, c := range candidates {
		cost := estimateUnitCost(c.Model, pricing)
		scored = append(scored, ScoreCandidate{
			Candidate: c,
			Score:     scoreCandidate(c.Model, cost, strategy, class),
			UnitCost:  cost,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		// Tie-break: cheaper first.
		return scored[i].UnitCost < scored[j].UnitCost
	})
	return scored
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
func scoreCandidate(model *core.Model, unitCost float64, strategy string, class Classification) float64 {
	tier := modelTier(model, unitCost)
	quality := tierQualityScore(tier) // cheap=1, standard=2, premium=3

	// Cost advantage: cheaper models get a higher cost score. Guard against
	// zero/unknown cost by assuming a mid price so unknowns land in the middle.
	costScore := 0.0
	switch {
	case unitCost <= 0:
		costScore = 0.5
	case unitCost < 1:
		costScore = 1.0 - unitCost // 0..1
	default:
		costScore = 0.0
	}

	switch strategy {
	case StrategyCost:
		return 10*costScore + 0.1*float64(quality) + capabilityBonus(model, class)
	case StrategyQuality:
		base := float64(quality)
		if class.RequiresReasoning || class.QualitySensitivity == "high" {
			base *= 1.5
		}
		return base + capabilityBonus(model, class)
	case StrategyLatency:
		// Prefer cheaper tiers as a latency proxy and small-context models.
		return 5*costScore + float64(4-quality) + capabilityBonus(model, class)
	default: // balanced
		return 4*costScore + float64(quality) + capabilityBonus(model, class)
	}
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
	default:
		return 1
	}
}

// modelTier derives a coarse price tier from pricing or metadata tags.
func modelTier(model *core.Model, unitCost float64) string {
	if model != nil && model.Metadata != nil {
		for _, tag := range model.Metadata.Tags {
			switch strings.ToLower(strings.TrimSpace(tag)) {
			case "premium", "frontier":
				return "premium"
			case "cheap", "mini", "flash", "haiku", "lite":
				return "cheap"
			}
		}
	}
	switch {
	case unitCost <= 0:
		return "standard"
	case unitCost < 1:
		return "cheap"
	case unitCost < 5:
		return "standard"
	default:
		return "premium"
	}
}

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
