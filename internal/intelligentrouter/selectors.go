package intelligentrouter

import (
	"strings"
)

// Default intelligent selector names. Each maps to a strategy unless overridden
// by config. "auto" is the general-purpose entry point.
const (
	SelectorAuto        = "auto"
	SelectorSmart       = "smart"
	SelectorAutoCost    = "auto-cost"
	SelectorAutoQuality = "auto-quality"
)

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
