package admin

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"gomodel/internal/core"
	"gomodel/internal/routing"
)

type routingPoolCandidateResponse struct {
	ProviderName         string `json:"provider_name"`
	ProviderType         string `json:"provider_type"`
	Model                string `json:"model"`
	Priority             int    `json:"priority"`
	Weight               int    `json:"weight"`
	ProviderEnabled      bool   `json:"provider_enabled"`
	CandidateEnabled     bool   `json:"candidate_enabled"`
	EffectiveEnabled     bool   `json:"effective_enabled"`
	Status               string `json:"status"`
	StatusReason         string `json:"status_reason,omitempty"`
	ProviderRuntime      string `json:"provider_runtime_status,omitempty"`
	ProviderLastError    string `json:"provider_last_error,omitempty"`
	IsConfigPrimary      bool   `json:"is_config_primary"`
	IsEffectiveCandidate bool   `json:"is_effective_candidate"`
}

type routingPoolResponse struct {
	CanonicalModel         string                         `json:"canonical_model"`
	Enabled                bool                           `json:"enabled"`
	Strategy               string                         `json:"strategy"`
	Status                 string                         `json:"status"`
	StatusReason           string                         `json:"status_reason,omitempty"`
	EffectiveCandidate     string                         `json:"effective_candidate,omitempty"`
	EffectiveProviderName  string                         `json:"effective_provider_name,omitempty"`
	ConfigPrimaryCandidate string                         `json:"config_primary_candidate,omitempty"`
	ConfigPrimaryProvider  string                         `json:"config_primary_provider_name,omitempty"`
	BlockedCandidates      []routingBlockedCandidate      `json:"blocked_candidates,omitempty"`
	Candidates             []routingPoolCandidateResponse `json:"candidates"`
}

type routingBlockedCandidate struct {
	Selector string `json:"selector"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

func (h *Handler) ListRoutingModelPools(c *echo.Context) error {
	pools := h.buildRoutingPoolResponses()
	if pools == nil {
		pools = []routingPoolResponse{}
	}
	return c.JSON(http.StatusOK, pools)
}

func (h *Handler) buildRoutingPoolResponses() []routingPoolResponse {
	if len(h.routingConfig.ModelPools) == 0 {
		return nil
	}

	runtimeByName := make(map[string]routing.CandidateRuntimeInfo)
	if h.registry != nil {
		for _, snapshot := range h.registry.ProviderRuntimeSnapshots() {
			name := strings.TrimSpace(snapshot.Name)
			if name == "" {
				continue
			}
			lastError := strings.TrimSpace(snapshot.LastModelFetchError)
			if lastError == "" {
				lastError = strings.TrimSpace(snapshot.LastAvailabilityError)
			}
			runtimeByName[name] = routing.CandidateRuntimeInfo{
				Status:    routing.ClassifyProviderRuntime(snapshot),
				LastError: lastError,
			}
		}
	}

	responses := make([]routingPoolResponse, 0, len(h.routingConfig.ModelPools))
	for canonical, poolCfg := range h.routingConfig.ModelPools {
		canonicalName := strings.TrimSpace(canonical)
		if canonicalName == "" {
			continue
		}

		pool := routing.Pool{CanonicalModel: canonicalName, Candidates: make([]routing.Candidate, 0, len(poolCfg.Candidates))}
		for _, candidate := range poolCfg.Candidates {
			pool.Candidates = append(pool.Candidates, routing.Candidate{
				Provider: strings.TrimSpace(candidate.Provider),
				Model:    strings.TrimSpace(candidate.Model),
				Priority: candidate.Priority,
				Weight:   candidate.Weight,
			})
		}

		evaluated := routing.EvaluatePool(h.routingConfig.Defaults.Strategy, pool, h.routingState, runtimeByName)
		configPrimary, _ := evaluated.ConfigPrimaryCandidate()
		effectiveCandidate := ""
		effectiveProviderName := ""
		if resolver := routing.NewResolver(h.routingConfig, h.routingState); resolver != nil {
			resolver = resolver.WithRuntime(h.registry)
			if resolution, matched, err := resolver.Resolve(core.NewRequestedModelSelector(canonicalName, "")); err == nil && matched && resolution != nil {
				effectiveCandidate = resolution.Primary.QualifiedModel()
				effectiveProviderName = resolution.Primary.Provider
			}
		}

		candidates := make([]routingPoolCandidateResponse, 0, len(evaluated.Candidates))
		for _, candidate := range evaluated.Candidates {
			providerType := ""
			if h.registry != nil {
				providerType = strings.TrimSpace(h.registry.GetProviderTypeForName(candidate.Candidate.Provider))
			}
			qualified := candidate.Candidate.QualifiedModel()
			candidates = append(candidates, routingPoolCandidateResponse{
				ProviderName:         candidate.Candidate.Provider,
				ProviderType:         providerType,
				Model:                candidate.Candidate.Model,
				Priority:             candidate.Candidate.Priority,
				Weight:               candidate.Candidate.Weight,
				ProviderEnabled:      candidate.ProviderEnabled,
				CandidateEnabled:     candidate.CandidateEnabled,
				EffectiveEnabled:     candidate.EffectiveEnabled,
				Status:               candidate.Status,
				StatusReason:         candidate.StatusReason,
				ProviderRuntime:      candidate.RuntimeStatus,
				ProviderLastError:    candidate.RuntimeLastError,
				IsConfigPrimary:      qualified == configPrimary.QualifiedModel(),
				IsEffectiveCandidate: qualified != "" && qualified == effectiveCandidate,
			})
		}

		blocked := make([]routingBlockedCandidate, 0, len(evaluated.BlockedCandidates()))
		for _, blockedCandidate := range evaluated.BlockedCandidates() {
			blocked = append(blocked, routingBlockedCandidate{
				Selector: blockedCandidate.Selector.QualifiedModel(),
				Status:   blockedCandidate.Status,
				Reason:   blockedCandidate.Reason,
			})
		}

		responses = append(responses, routingPoolResponse{
			CanonicalModel:         canonicalName,
			Enabled:                evaluated.Enabled,
			Strategy:               string(evaluated.Strategy),
			Status:                 evaluated.Status,
			StatusReason:           evaluated.StatusReason,
			EffectiveCandidate:     effectiveCandidate,
			EffectiveProviderName:  effectiveProviderName,
			ConfigPrimaryCandidate: configPrimary.QualifiedModel(),
			ConfigPrimaryProvider:  configPrimary.Provider,
			BlockedCandidates:      blocked,
			Candidates:             candidates,
		})
	}
	return responses
}
