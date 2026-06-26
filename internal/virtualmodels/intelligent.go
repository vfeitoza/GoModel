package virtualmodels

import (
	"strings"

	"gomodel/internal/core"
)

// IntelligentTargets returns candidate selectors declared by an intelligent
// virtual model. It returns ok=false when source is not an enabled intelligent
// redirect or when user_path scoping does not match.
func (s *Service) IntelligentTargets(source, userPath string) ([]core.ModelSelector, string, bool) {
	if s == nil {
		return nil, "", false
	}
	vm, _, ok := s.snapshot().lookupCanonicalSource(source)
	if !ok || !vm.Enabled || !vm.IsRedirect() || !isIntelligentStrategy(vm.Strategy) {
		return nil, "", false
	}
	if len(vm.UserPaths) > 0 && !userPathAllowed(userPath, vm.UserPaths) {
		return nil, "", false
	}
	targets := make([]core.ModelSelector, 0, len(vm.Targets))
	for _, target := range vm.Targets {
		selector, err := target.selector()
		if err != nil {
			continue
		}
		targets = append(targets, selector)
	}
	if len(targets) == 0 {
		return nil, "", false
	}
	strategy := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(vm.Strategy)), "intelligent")
	strategy = strings.TrimPrefix(strategy, ":")
	if strategy == "" {
		strategy = "balanced"
	}
	return targets, strategy, true
}
