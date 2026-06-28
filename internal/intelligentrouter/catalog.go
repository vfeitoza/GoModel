package intelligentrouter

import (
	"strings"

	"gomodel/internal/core"
)

// Candidate is a catalog model eligible for selection.
type Candidate struct {
	Selector     core.ModelSelector
	Provider     string // configured provider name
	Model        *core.Model
	ContextScore float64 // 1.0 = comfortable fit, decays toward 0.10 near the limit, 0.0 = excluded
}

// CandidateFilter holds allow/deny glob patterns over qualified selectors.
type CandidateFilter struct {
	Allow []string
	Deny  []string
}

// BuildCandidates lists catalog models eligible for the classification. Models
// are filtered by the allow/deny patterns and by hard capability requirements
// implied by the classification (vision, long context, tools).
func BuildCandidates(catalog Catalog, filter CandidateFilter, allowOverride []string, class Classification, requestedContextChars int) []Candidate {
	if catalog == nil {
		return nil
	}
	// An explicit override (intelligent virtual model targets) replaces Allow.
	allow := filter.Allow
	if len(allowOverride) > 0 {
		allow = allowOverride
	}

	var out []Candidate
	for _, model := range catalog.ListModels() {
		if model.ID == "" {
			continue
		}
		if !modelSupportsChat(model) {
			continue
		}
		provider := providerNameForModel(catalog, model.ID)
		selector := core.ModelSelector{Model: model.ID, Provider: provider}
		qualified := selector.QualifiedModel()

		if matchesAny(qualified, model.ID, filter.Deny) {
			continue
		}
		if len(allow) > 0 && !matchesAny(qualified, model.ID, allow) {
			continue
		}
		if class.RequiresVision && !modelSupportsVision(model) {
			continue
		}
		if class.RequiresTools && !modelSupportsTools(model) {
			continue
		}
		// Hard gate for declared long-context requirements: a model that
		// advertises a window below 64k cannot serve a request the analyzer
		// flagged as needing long context.
		if class.RequiresLongContext && !modelSupportsLongContext(model) {
			continue
		}
		// Gradual context fit: requests that approach or exceed a model's
		// window receive a proportional penalty (or are hard-excluded when they
		// cannot fit at all). Unknown windows are never penalized.
		estimatedTokens := requestedContextChars / 4
		ctxScore := contextWindowScore(model, estimatedTokens)
		if ctxScore <= 0 {
			continue
		}
		out = append(out, Candidate{Selector: selector, Provider: provider, Model: &model, ContextScore: ctxScore})
	}
	return out
}

// modelSupportsLongContext reports whether the model advertises a context
// window of at least 64k tokens, used as a proxy for "long context capable".
func modelSupportsLongContext(model core.Model) bool {
	if model.Metadata == nil || model.Metadata.ContextWindow == nil {
		return true // unknown → do not exclude
	}
	return *model.Metadata.ContextWindow >= 64000
}

func modelSupportsChat(model core.Model) bool {
	// Models without metadata are assumed chat-capable (registry not enriched).
	if model.Metadata == nil || len(model.Metadata.Modes) == 0 {
		return true
	}
	for _, mode := range model.Metadata.Modes {
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "chat", "completion", "responses":
			return true
		}
	}
	return false
}

func modelSupportsVision(model core.Model) bool {
	return capability(model, "vision") || capability(model, "image_input")
}

func modelSupportsTools(model core.Model) bool {
	return capability(model, "tools") || capability(model, "tool_use")
}

// contextWindowScore returns a gradual fit score (0.0–1.0) for a model against
// the estimated token count of the request. Unlike a binary exclude, requests
// that approach a model's context window receive a proportional penalty instead
// of being dropped outright:
//
//   - unknown window        → 1.0 (never penalize what we don't know)
//   - estimated >= window   → 0.0 (hard exclude — request cannot fit)
//   - usage > 80% of window → linear decay from 1.0 down to 0.10
//   - usage <= 80%          → 1.0 (comfortable fit)
//
// estimatedTokens is the caller's best guess (chars/4 is a coarse approximation
// of GPT tokenization). When the caller passes 0, no scoring is applied and the
// model receives 1.0.
func contextWindowScore(model core.Model, estimatedTokens int) float64 {
	if model.Metadata == nil || model.Metadata.ContextWindow == nil || estimatedTokens <= 0 {
		return 1.0
	}
	window := *model.Metadata.ContextWindow
	if window <= 0 {
		return 1.0
	}
	if estimatedTokens >= window {
		return 0.0
	}
	usage := float64(estimatedTokens) / float64(window)
	if usage <= contextWarnThreshold {
		return 1.0
	}
	// Linear decay in the risk zone [warnThreshold, 1.0) from 1.0 down to minScore.
	t := (usage - contextWarnThreshold) / (1.0 - contextWarnThreshold)
	return 1.0 - t*(1.0-contextMinScore)
}

const (
	contextWarnThreshold = 0.80 // above 80% of the window, the penalty begins
	contextMinScore      = 0.10 // lowest non-excluded score, near the limit
)

func capability(model core.Model, key string) bool {
	if model.Metadata == nil || model.Metadata.Capabilities == nil {
		return false
	}
	return model.Metadata.Capabilities[key]
}

func providerNameForModel(catalog Catalog, modelID string) string {
	if lookup, ok := catalog.(interface {
		GetProviderName(model string) string
	}); ok {
		return strings.TrimSpace(lookup.GetProviderName(modelID))
	}
	return ""
}

// matchesAny reports whether the qualified selector or bare model id matches
// any pattern. Patterns support a trailing "*" wildcard.
func matchesAny(qualified, modelID string, patterns []string) bool {
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if globMatch(p, qualified) || globMatch(p, modelID) {
			return true
		}
	}
	return false
}

func globMatch(pattern, value string) bool {
	if pattern == value {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	return false
}
