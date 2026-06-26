package intelligentrouter

import (
	"strings"

	"gomodel/internal/core"
)

// Candidate is a catalog model eligible for selection.
type Candidate struct {
	Selector core.ModelSelector
	Provider string // configured provider name
	Model    *core.Model
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
		if class.RequiresLongContext && !modelSupportsLongContext(model) {
			continue
		}
		out = append(out, Candidate{Selector: selector, Provider: provider, Model: &model})
	}
	return out
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

// modelSupportsLongContext reports whether the model advertises a context
// window of at least 64k tokens, used as a proxy for "long context capable".
func modelSupportsLongContext(model core.Model) bool {
	if model.Metadata == nil || model.Metadata.ContextWindow == nil {
		return true // unknown → do not exclude
	}
	return *model.Metadata.ContextWindow >= 64000
}

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
