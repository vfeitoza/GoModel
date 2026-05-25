package config

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type RoutingStrategy string

const (
	RoutingStrategyPriorityFailover   RoutingStrategy = "priority_failover"
	RoutingStrategyWeightedRoundRobin RoutingStrategy = "weighted_round_robin"
)

func normalizeRoutingStrategy(strategy RoutingStrategy) RoutingStrategy {
	return RoutingStrategy(strings.ToLower(strings.TrimSpace(string(strategy))))
}

func ResolveRoutingStrategy(strategy RoutingStrategy) RoutingStrategy {
	strategy = normalizeRoutingStrategy(strategy)
	if strategy == "" {
		return RoutingStrategyPriorityFailover
	}
	return strategy
}

func (s RoutingStrategy) Valid() bool {
	switch normalizeRoutingStrategy(s) {
	case RoutingStrategyPriorityFailover, RoutingStrategyWeightedRoundRobin:
		return true
	default:
		return false
	}
}

// RoutingConfig holds canonical model pool routing configuration.
type RoutingConfig struct {
	Defaults   RoutingDefaultsConfig       `yaml:"defaults"`
	ModelPools map[string]ModelPoolConfig `yaml:"model_pools"`
}

// RoutingDefaultsConfig holds default routing behavior for canonical pools.
type RoutingDefaultsConfig struct {
	Strategy          RoutingStrategy      `yaml:"strategy"`
	SessionAffinity   bool                 `yaml:"session_affinity"`
	SessionAffinityTTL time.Duration       `yaml:"session_affinity_ttl"`
	Failover          RoutingFailoverConfig `yaml:"failover"`
}

// RoutingFailoverConfig controls fallback between candidates within the same pool.
type RoutingFailoverConfig struct {
	Enabled            bool  `yaml:"enabled"`
	MaxAttempts        int   `yaml:"max_attempts"`
	RetryOnStatuses    []int `yaml:"retry_on_statuses"`
	RetryOnModelErrors bool  `yaml:"retry_on_model_errors"`
}

// ModelPoolConfig maps one public canonical model name to concrete provider candidates.
type ModelPoolConfig struct {
	Candidates []ModelPoolCandidateConfig `yaml:"candidates"`
}

// ModelPoolCandidateConfig defines one concrete provider/model candidate.
type ModelPoolCandidateConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Priority int    `yaml:"priority"`
	Weight   int    `yaml:"weight"`
}

func loadRoutingConfig(cfg *RoutingConfig) error {
	if cfg == nil {
		return nil
	}

	cfg.Defaults.Strategy = ResolveRoutingStrategy(cfg.Defaults.Strategy)
	if !cfg.Defaults.Strategy.Valid() {
		return fmt.Errorf("routing.defaults.strategy must be one of: priority_failover, weighted_round_robin")
	}
	if cfg.Defaults.SessionAffinityTTL <= 0 {
		cfg.Defaults.SessionAffinityTTL = 30 * time.Minute
	}
	if cfg.Defaults.Failover.MaxAttempts <= 0 {
		cfg.Defaults.Failover.MaxAttempts = 3
	}
	if len(cfg.Defaults.Failover.RetryOnStatuses) == 0 {
		cfg.Defaults.Failover.RetryOnStatuses = []int{429, 500, 502, 503, 504}
	}

	if len(cfg.ModelPools) == 0 {
		cfg.ModelPools = nil
		return nil
	}

	normalized := make(map[string]ModelPoolConfig, len(cfg.ModelPools))
	keys := make([]string, 0, len(cfg.ModelPools))
	for key := range cfg.ModelPools {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			return fmt.Errorf("routing.model_pools: model key cannot be empty")
		}
		if _, exists := normalized[trimmedKey]; exists {
			return fmt.Errorf("routing.model_pools: duplicate model key after trimming: %q", trimmedKey)
		}
		pool := cfg.ModelPools[key]
		if len(pool.Candidates) == 0 {
			return fmt.Errorf("routing.model_pools[%q]: at least one candidate is required", trimmedKey)
		}

		seenCandidates := make(map[string]struct{}, len(pool.Candidates))
		normalizedCandidates := make([]ModelPoolCandidateConfig, 0, len(pool.Candidates))
		for idx, candidate := range pool.Candidates {
			candidate.Provider = strings.TrimSpace(candidate.Provider)
			candidate.Model = strings.TrimSpace(candidate.Model)
			if candidate.Provider == "" {
				return fmt.Errorf("routing.model_pools[%q].candidates[%d].provider is required", trimmedKey, idx)
			}
			if candidate.Model == "" {
				return fmt.Errorf("routing.model_pools[%q].candidates[%d].model is required", trimmedKey, idx)
			}
			candidateKey := candidate.Provider + "/" + candidate.Model
			if _, exists := seenCandidates[candidateKey]; exists {
				return fmt.Errorf("routing.model_pools[%q]: duplicate candidate %q", trimmedKey, candidateKey)
			}
			seenCandidates[candidateKey] = struct{}{}

			switch cfg.Defaults.Strategy {
			case RoutingStrategyPriorityFailover:
				if candidate.Priority <= 0 {
					return fmt.Errorf("routing.model_pools[%q].candidates[%d].priority must be > 0 for priority_failover", trimmedKey, idx)
				}
			case RoutingStrategyWeightedRoundRobin:
				if candidate.Weight <= 0 {
					return fmt.Errorf("routing.model_pools[%q].candidates[%d].weight must be > 0 for weighted_round_robin", trimmedKey, idx)
				}
			}

			normalizedCandidates = append(normalizedCandidates, candidate)
		}
		normalized[trimmedKey] = ModelPoolConfig{Candidates: normalizedCandidates}
	}

	cfg.ModelPools = normalized
	return nil
}
