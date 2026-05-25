package routing

import (
	"sort"
	"strings"

	"gomodel/config"
	"gomodel/internal/core"
)

type Catalog interface {
	LookupModel(model string) (*core.Model, bool)
}

type CanonicalExposedModelLister struct {
	cfg      config.RoutingConfig
	catalog  Catalog
	state    StateChecker
	runtime  RuntimeSnapshotProvider
	resolver *Resolver
}

func NewCanonicalExposedModelLister(cfg config.RoutingConfig, catalog Catalog, state StateChecker, runtime RuntimeSnapshotProvider) *CanonicalExposedModelLister {
	if len(cfg.ModelPools) == 0 || catalog == nil {
		return nil
	}
	return &CanonicalExposedModelLister{cfg: cfg, catalog: catalog, state: state, runtime: runtime, resolver: NewResolver(cfg, state).WithRuntime(runtime)}
}

func (l *CanonicalExposedModelLister) ExposedModels() []core.Model {
	return l.ExposedModelsFiltered(nil)
}

func (l *CanonicalExposedModelLister) ExposedModelsFiltered(allow func(core.ModelSelector) bool) []core.Model {
	if l == nil || l.catalog == nil || len(l.cfg.ModelPools) == 0 {
		return nil
	}
	models := make([]core.Model, 0, len(l.cfg.ModelPools))
	for canonical := range l.cfg.ModelPools {
		canonical = strings.TrimSpace(canonical)
		if canonical == "" {
			continue
		}
		if l.resolver == nil {
			continue
		}
		resolution, matched, err := l.resolver.Resolve(core.NewRequestedModelSelector(canonical, ""))
		if err != nil || !matched || resolution == nil {
			continue
		}
		selector := resolution.Primary
		if allow != nil && !allow(selector) {
			continue
		}
		model, ok := l.catalog.LookupModel(selector.QualifiedModel())
		if !ok || model == nil {
			continue
		}
		clone := *model
		clone.ID = canonical
		models = append(models, clone)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

type CombinedExposedModelLister struct {
	listers []serverExposedModelLister
}

type serverExposedModelLister interface {
	ExposedModels() []core.Model
}

type serverFilteredExposedModelLister interface {
	ExposedModelsFiltered(allow func(core.ModelSelector) bool) []core.Model
}

func NewCombinedExposedModelLister(listers ...serverExposedModelLister) *CombinedExposedModelLister {
	filtered := make([]serverExposedModelLister, 0, len(listers))
	for _, lister := range listers {
		if lister != nil {
			filtered = append(filtered, lister)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return &CombinedExposedModelLister{listers: filtered}
}

func (l *CombinedExposedModelLister) ExposedModels() []core.Model {
	if l == nil {
		return nil
	}
	var result []core.Model
	for _, lister := range l.listers {
		result = append(result, lister.ExposedModels()...)
	}
	return result
}

func (l *CombinedExposedModelLister) ExposedModelsFiltered(allow func(core.ModelSelector) bool) []core.Model {
	if l == nil {
		return nil
	}
	var result []core.Model
	for _, lister := range l.listers {
		if filtered, ok := lister.(serverFilteredExposedModelLister); ok {
			result = append(result, filtered.ExposedModelsFiltered(allow)...)
			continue
		}
		result = append(result, lister.ExposedModels()...)
	}
	return result
}
