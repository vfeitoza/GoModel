package routing

import (
	"strings"

	"gomodel/config"
	"gomodel/internal/core"
)

type CandidateRuntimeInfo struct {
	Status    string
	LastError string
}

type EvaluatedCandidate struct {
	Candidate        Candidate
	ProviderEnabled  bool
	CandidateEnabled bool
	EffectiveEnabled bool
	Selectable       bool
	Status           string
	StatusReason     string
	RuntimeStatus    string
	RuntimeLastError string
}

type EvaluatedPool struct {
	CanonicalModel string
	Strategy       config.RoutingStrategy
	Enabled        bool
	Status         string
	StatusReason   string
	Candidates     []EvaluatedCandidate
}

func (p EvaluatedPool) ConfigPrimaryCandidate() (Candidate, bool) {
	if len(p.Candidates) == 0 {
		return Candidate{}, false
	}
	candidate := p.Candidates[0].Candidate
	for _, current := range p.Candidates[1:] {
		switch p.Strategy {
		case config.RoutingStrategyWeightedRoundRobin:
			if current.Candidate.Weight > candidate.Weight || (current.Candidate.Weight == candidate.Weight && current.Candidate.Priority < candidate.Priority) {
				candidate = current.Candidate
			}
		default:
			if current.Candidate.Priority < candidate.Priority || (current.Candidate.Priority == candidate.Priority && current.Candidate.QualifiedModel() < candidate.QualifiedModel()) {
				candidate = current.Candidate
			}
		}
	}
	return candidate, true
}

func (p EvaluatedPool) BlockedCandidates() []core.BlockedCandidate {
	blocked := make([]core.BlockedCandidate, 0)
	for _, candidate := range p.Candidates {
		if candidate.Selectable {
			continue
		}
		blocked = append(blocked, core.BlockedCandidate{Selector: candidate.Candidate.Selector(), Reason: candidate.StatusReason, Status: candidate.Status})
	}
	return blocked
}

type detailedStateChecker interface {
	StateChecker
	ProviderEnabled(name string) bool
	CandidateEnabled(selector core.ModelSelector) bool
}

func EvaluatePool(strategy config.RoutingStrategy, pool Pool, state StateChecker, runtimeByProvider map[string]CandidateRuntimeInfo) EvaluatedPool {
	strategy = config.ResolveRoutingStrategy(strategy)
	canonicalEnabled := true
	if state != nil {
		canonicalEnabled = state.CanonicalModelEnabled(pool.CanonicalModel)
	}

	result := EvaluatedPool{
		CanonicalModel: pool.CanonicalModel,
		Strategy:       strategy,
		Enabled:        canonicalEnabled,
		Candidates:     make([]EvaluatedCandidate, 0, len(pool.Candidates)),
	}

	selectableCount := 0
	availableCount := 0
	for _, candidate := range pool.Candidates {
		providerEnabled := providerEnabled(state, candidate.Provider)
		candidateEnabled := candidateEnabled(state, candidate.Selector())
		effectiveEnabled := canonicalEnabled && providerEnabled && candidateEnabled
		runtime := runtimeByProvider[strings.TrimSpace(candidate.Provider)]

		status := "enabled"
		reason := ""
		switch {
		case !candidateEnabled:
			status = "disabled_manual"
			reason = "candidate disabled manually"
		case !providerEnabled:
			status = "disabled_manual"
			reason = "provider disabled manually"
		case !canonicalEnabled:
			status = "disabled_effective"
			reason = "canonical model disabled manually"
		case runtime.Status == "degraded" || runtime.Status == "unhealthy":
			status = "degraded_runtime"
			reason = "provider runtime degraded"
		}

		selectable := effectiveEnabled && runtime.Status != "unhealthy"
		if selectable {
			selectableCount++
		}
		if effectiveEnabled {
			availableCount++
		}

		result.Candidates = append(result.Candidates, EvaluatedCandidate{
			Candidate:        candidate,
			ProviderEnabled:  providerEnabled,
			CandidateEnabled: candidateEnabled,
			EffectiveEnabled: effectiveEnabled,
			Selectable:       selectable,
			Status:           status,
			StatusReason:     reason,
			RuntimeStatus:    runtime.Status,
			RuntimeLastError: runtime.LastError,
		})
	}

	switch {
	case !canonicalEnabled:
		result.Status = "disabled_manual"
		result.StatusReason = "canonical model disabled manually"
	case selectableCount == 0:
		result.Status = "degraded"
		result.StatusReason = "no enabled candidates"
	case availableCount < len(result.Candidates):
		result.Status = "degraded"
		result.StatusReason = "one or more candidates unavailable"
	default:
		result.Status = "enabled"
	}

	return result
}

func (p EvaluatedPool) SelectableCandidates() []Candidate {
	candidates := make([]Candidate, 0, len(p.Candidates))
	for _, candidate := range p.Candidates {
		if candidate.Selectable {
			candidates = append(candidates, candidate.Candidate)
		}
	}
	return candidates
}

func providerEnabled(state StateChecker, provider string) bool {
	if state == nil {
		return true
	}
	if detailed, ok := state.(detailedStateChecker); ok {
		return detailed.ProviderEnabled(provider)
	}
	return true
}

func candidateEnabled(state StateChecker, selector core.ModelSelector) bool {
	if state == nil {
		return true
	}
	if detailed, ok := state.(detailedStateChecker); ok {
		return detailed.CandidateEnabled(selector)
	}
	return true
}
