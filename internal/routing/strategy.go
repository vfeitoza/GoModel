package routing

import (
	"fmt"
	"sort"

	"gomodel/config"
)

func selectCandidates(strategy config.RoutingStrategy, pool Pool, counters map[string]int) (Candidate, []Candidate, error) {
	switch strategy {
	case config.RoutingStrategyPriorityFailover:
		ordered := append([]Candidate(nil), pool.Candidates...)
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].Priority != ordered[j].Priority {
				return ordered[i].Priority < ordered[j].Priority
			}
			return ordered[i].QualifiedModel() < ordered[j].QualifiedModel()
		})
		return ordered[0], ordered[1:], nil
	case config.RoutingStrategyWeightedRoundRobin:
		return selectWeightedRoundRobin(pool, counters)
	default:
		return Candidate{}, nil, fmt.Errorf("unsupported routing strategy: %s", strategy)
	}
}

func selectWeightedRoundRobin(pool Pool, counters map[string]int) (Candidate, []Candidate, error) {
	if len(pool.Candidates) == 0 {
		return Candidate{}, nil, fmt.Errorf("pool %q has no candidates", pool.CanonicalModel)
	}

	ordered := append([]Candidate(nil), pool.Candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Weight != ordered[j].Weight {
			return ordered[i].Weight > ordered[j].Weight
		}
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority < ordered[j].Priority
		}
		return ordered[i].QualifiedModel() < ordered[j].QualifiedModel()
	})

	bestIdx := 0
	bestScore := 0
	for i, candidate := range ordered {
		key := pool.CanonicalModel + "|" + candidate.QualifiedModel()
		score := candidate.Weight - counters[key]
		if i == 0 || score > bestScore {
			bestIdx = i
			bestScore = score
		}
	}

	primary := ordered[bestIdx]
	primaryKey := pool.CanonicalModel + "|" + primary.QualifiedModel()
	counters[primaryKey]++

	fallbacks := make([]Candidate, 0, len(ordered)-1)
	for i, candidate := range ordered {
		if i == bestIdx {
			continue
		}
		fallbacks = append(fallbacks, candidate)
	}
	return primary, fallbacks, nil
}
