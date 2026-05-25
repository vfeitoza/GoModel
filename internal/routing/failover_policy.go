package routing

import (
	"errors"
	"net/http"
	"strings"

	"gomodel/config"
	"gomodel/internal/core"
)

type FailoverPolicy struct {
	Enabled            bool
	MaxAttempts        int
	RetryOnStatuses    map[int]struct{}
	RetryOnModelErrors bool
}

func NewFailoverPolicy(cfg config.RoutingFailoverConfig) FailoverPolicy {
	statuses := make(map[int]struct{}, len(cfg.RetryOnStatuses))
	for _, status := range cfg.RetryOnStatuses {
		statuses[status] = struct{}{}
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return FailoverPolicy{
		Enabled:            cfg.Enabled,
		MaxAttempts:        maxAttempts,
		RetryOnStatuses:    statuses,
		RetryOnModelErrors: cfg.RetryOnModelErrors,
	}
}

func (p FailoverPolicy) ShouldAttempt(err error) bool {
	if !p.Enabled {
		return false
	}
	if err == nil {
		return false
	}
	var gatewayErr *core.GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr == nil {
		return false
	}
	status := gatewayErr.HTTPStatusCode()
	if _, ok := p.RetryOnStatuses[status]; ok {
		return true
	}
	if !p.RetryOnModelErrors {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(gatewayErr.Message))
	if strings.Contains(message, "model") && (strings.Contains(message, "unavailable") || strings.Contains(message, "not found") || strings.Contains(message, "unsupported")) {
		return true
	}
	if status >= http.StatusInternalServerError || status == http.StatusTooManyRequests {
		return true
	}
	return false
}
