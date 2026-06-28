package intelligentrouter

import (
	"sort"
	"strings"

	"gomodel/internal/core"
)

// Default intelligent selector names. Each maps to a strategy unless overridden
// by config. "auto" is the general-purpose entry point.
const (
	SelectorAuto        = "auto"
	SelectorSmart       = "smart"
	SelectorAutoCost    = "auto-cost"
	SelectorAutoQuality = "auto-quality"
)

// DefaultSelectorNames is the ordered set of built-in intelligent selectors,
// used when none are explicitly configured.
var DefaultSelectorNames = []string{
	SelectorAuto,
	SelectorSmart,
	SelectorAutoCost,
	SelectorAutoQuality,
}

// defaultSelectorStrategy is the strategy applied when no per-selector override
// or SelectionMeta.Strategy is set.
var defaultSelectorStrategy = map[string]string{
	SelectorAuto:        StrategyBalanced,
	SelectorSmart:       StrategyBalanced,
	SelectorAutoCost:    StrategyCost,
	SelectorAutoQuality: StrategyQuality,
}

// IsIntelligentSelector reports whether model is an intelligent selector name
// and, when it is, returns its default strategy. The provider hint is ignored:
// intelligent selectors are never provider-qualified.
func IsIntelligentSelector(model string) (strategy string, ok bool) {
	name := strings.ToLower(strings.TrimSpace(model))
	strategy, ok = defaultSelectorStrategy[name]
	return strategy, ok
}

// SelectorsAsModels projects selector names into model-list entries suitable
// for inclusion in GET /v1/models. Empty/whitespace names are dropped and the
// result is sorted by ID. Each entry is tagged owned_by "intelligent-router"
// so clients can distinguish a virtual selector from a concrete provider model.
func SelectorsAsModels(names []string) []core.Model {
	seen := make(map[string]struct{}, len(names))
	out := make([]core.Model, 0, len(names))
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		strategy, _ := defaultSelectorStrategy[name]
		out = append(out, core.Model{
			ID:      name,
			Object:  "model",
			OwnedBy: "intelligent-router",
			Metadata: &core.ModelMetadata{
				DisplayName: "Intelligent · " + name,
				Description: selectorDescription(name, strategy),
			},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// selectorDescription returns a short human-readable description for a selector.
// Known built-ins get tailored copy; operator-configured selectors fall back to
// a generic description that still signals what the entry is.
func selectorDescription(name, strategy string) string {
	switch name {
	case SelectorAuto:
		return "Automatically selects the best model for each request (balanced cost and quality)"
	case SelectorSmart:
		return "Alias for auto — balanced cost and quality"
	case SelectorAutoCost:
		return "Selects the cheapest eligible model for the request"
	case SelectorAutoQuality:
		return "Selects the highest-quality eligible model for the request"
	}
	desc := "Intelligent selector configured by operator"
	if strategy != "" {
		desc += " (" + strategy + " strategy)"
	}
	return desc
}
