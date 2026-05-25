package routing

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"gomodel/config"
	"gomodel/internal/core"
)

type StateChecker interface {
	CanonicalModelEnabled(name string) bool
	FilterCandidates(canonical string, candidates []Candidate) []Candidate
}

type Resolver struct {
	strategy config.RoutingStrategy
	pools    map[string]Pool
	state    StateChecker
	runtime  RuntimeSnapshotProvider
	affinity *affinityStore
	mu       sync.Mutex
	counters map[string]int
}

func NewResolver(cfg config.RoutingConfig, state ...StateChecker) *Resolver {
	if len(cfg.ModelPools) == 0 {
		return nil
	}

	pools := make(map[string]Pool, len(cfg.ModelPools))
	for canonicalModel, poolCfg := range cfg.ModelPools {
		candidates := make([]Candidate, 0, len(poolCfg.Candidates))
		for _, candidate := range poolCfg.Candidates {
			candidates = append(candidates, Candidate{
				Provider: strings.TrimSpace(candidate.Provider),
				Model:    strings.TrimSpace(candidate.Model),
				Priority: candidate.Priority,
				Weight:   candidate.Weight,
			})
		}
		key := normalizePoolKey(canonicalModel)
		pools[key] = Pool{CanonicalModel: key, Candidates: candidates}
	}

	var checker StateChecker
	if len(state) > 0 {
		checker = state[0]
	}
	return &Resolver{
		strategy: config.ResolveRoutingStrategy(cfg.Defaults.Strategy),
		pools:    pools,
		state:    checker,
		affinity: newAffinityStore(cfg.Defaults.SessionAffinity, cfg.Defaults.SessionAffinityTTL),
		counters: make(map[string]int),
	}
}

func (r *Resolver) WithRuntime(runtime RuntimeSnapshotProvider) *Resolver {
	if r == nil {
		return nil
	}
	r.runtime = runtime
	return r
}

func (r *Resolver) HasPool(model string) bool {
	if r == nil {
		return false
	}
	_, ok := r.pools[normalizePoolKey(model)]
	return ok
}

func (r *Resolver) Resolve(requested core.RequestedModelSelector) (*core.CanonicalRoutingResolution, bool, error) {
	return r.ResolveWithContext(context.Background(), requested)
}

func (r *Resolver) ResolveWithContext(ctx context.Context, requested core.RequestedModelSelector) (*core.CanonicalRoutingResolution, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	if strings.TrimSpace(requested.ProviderHint) != "" {
		return nil, false, nil
	}

	pool, ok := r.pools[normalizePoolKey(requested.Model)]
	if !ok {
		return nil, false, nil
	}
	if len(pool.Candidates) == 0 {
		return nil, false, fmt.Errorf("routing pool %q has no candidates", pool.CanonicalModel)
	}

	evaluated := EvaluatePool(r.strategy, pool, r.state, RuntimeInfoByProvider(r.runtime))
	if !evaluated.Enabled {
		return nil, false, fmt.Errorf("canonical model %q is disabled", pool.CanonicalModel)
	}
	configPrimary, _ := evaluated.ConfigPrimaryCandidate()
	blockedCandidates := evaluated.BlockedCandidates()
	pool.Candidates = evaluated.SelectableCandidates()
	if len(pool.Candidates) == 0 {
		return nil, false, fmt.Errorf("routing pool %q has no enabled candidates", pool.CanonicalModel)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if affinity := r.affinity; affinity != nil {
		if pinned, ok := affinity.Get(ctx, pool.CanonicalModel); ok {
			for idx, candidate := range pool.Candidates {
				if candidate.QualifiedModel() != pinned.QualifiedModel() {
					continue
				}
				fallbacks := make([]Candidate, 0, len(pool.Candidates)-1)
				for i, fallback := range pool.Candidates {
					if i == idx {
						continue
					}
					fallbacks = append(fallbacks, fallback)
				}
				resolved := &core.CanonicalRoutingResolution{
					CanonicalModel:       pool.CanonicalModel,
					Primary:              candidate.Selector(),
					Strategy:             string(r.strategy),
					ConfigPrimary:        configPrimary.Selector(),
					EffectiveCandidate:   candidate.Selector(),
					SelectedExactModel:   candidate.Model,
					SelectedProviderName: candidate.Provider,
					BlockedCandidates:    append([]core.BlockedCandidate(nil), blockedCandidates...),
					Fallbacks:            make([]core.ModelSelector, 0, len(fallbacks)),
				}
				for _, fallback := range fallbacks {
					resolved.Fallbacks = append(resolved.Fallbacks, fallback.Selector())
				}
				return resolved, true, nil
			}
		}
	}
	primary, fallbacks, err := selectCandidates(r.strategy, pool, r.counters)
	if err == nil && r.affinity != nil {
		r.affinity.Put(ctx, pool.CanonicalModel, primary)
	}
	if err != nil {
		return nil, false, err
	}

	resolved := &core.CanonicalRoutingResolution{
		CanonicalModel:       pool.CanonicalModel,
		Primary:              primary.Selector(),
		Strategy:             string(r.strategy),
		ConfigPrimary:        configPrimary.Selector(),
		EffectiveCandidate:   primary.Selector(),
		SelectedProviderName: primary.Provider,
		SelectedExactModel:   primary.Model,
		BlockedCandidates:    append([]core.BlockedCandidate(nil), blockedCandidates...),
		Fallbacks:            make([]core.ModelSelector, 0, len(fallbacks)),
	}
	for _, candidate := range fallbacks {
		resolved.Fallbacks = append(resolved.Fallbacks, candidate.Selector())
	}
	return resolved, true, nil
}
