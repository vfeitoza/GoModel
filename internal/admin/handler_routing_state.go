package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"gomodel/internal/core"
	"gomodel/internal/routingstate"
)

type upsertRoutingStateRequest struct {
	Kind           string `json:"kind"`
	ProviderName   string `json:"provider_name,omitempty"`
	CanonicalModel string `json:"canonical_model,omitempty"`
	Model          string `json:"model,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type deleteRoutingStateRequest struct {
	Key string `json:"key"`
}

func (h *Handler) ListRoutingState(c *echo.Context) error {
	if h.routingState == nil {
		return handleError(c, featureUnavailableError("routing state feature is unavailable"))
	}
	views := h.routingState.List()
	if views == nil {
		views = []routingstate.Entry{}
	}
	return c.JSON(http.StatusOK, views)
}

func (h *Handler) UpsertRoutingState(c *echo.Context) error {
	if h.routingState == nil {
		return handleError(c, featureUnavailableError("routing state feature is unavailable"))
	}
	var req upsertRoutingStateRequest
	if err := c.Bind(&req); err != nil {
		return handleError(c, core.NewInvalidRequestError("invalid request body: "+err.Error(), err))
	}
	if req.Enabled == nil {
		return handleError(c, core.NewInvalidRequestError("enabled is required", nil))
	}
	entry := routingstate.Entry{
		Kind:           routingstate.Kind(strings.TrimSpace(req.Kind)),
		ProviderName:   strings.TrimSpace(req.ProviderName),
		CanonicalModel: strings.TrimSpace(req.CanonicalModel),
		Model:          strings.TrimSpace(req.Model),
		Enabled:        *req.Enabled,
		Reason:         strings.TrimSpace(req.Reason),
	}
	if err := h.routingState.Upsert(c.Request().Context(), entry); err != nil {
		if routingstate.IsValidationError(err) {
			return handleError(c, core.NewInvalidRequestError(err.Error(), err))
		}
		return handleError(c, core.NewProviderError("routing_state", http.StatusBadGateway, "failed to update routing state", err))
	}
	entries := h.routingState.List()
	for _, current := range entries {
		if current.Kind == entry.Kind {
			switch current.Kind {
			case routingstate.KindProvider:
				if current.ProviderName == entry.ProviderName {
					return c.JSON(http.StatusOK, current)
				}
			case routingstate.KindCanonicalModel:
				if current.CanonicalModel == entry.CanonicalModel {
					return c.JSON(http.StatusOK, current)
				}
			case routingstate.KindPoolCandidate:
				if current.ProviderName == entry.ProviderName && current.Model == entry.Model {
					return c.JSON(http.StatusOK, current)
				}
			}
		}
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) DeleteRoutingState(c *echo.Context) error {
	if h.routingState == nil {
		return handleError(c, featureUnavailableError("routing state feature is unavailable"))
	}
	var req deleteRoutingStateRequest
	if err := c.Bind(&req); err != nil {
		return handleError(c, core.NewInvalidRequestError("invalid request body: "+err.Error(), err))
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return handleError(c, core.NewInvalidRequestError("key is required", nil))
	}
	if err := h.routingState.Delete(c.Request().Context(), key); err != nil {
		if errors.Is(err, routingstate.ErrNotFound) {
			return handleError(c, core.NewNotFoundError("routing state not found: "+key))
		}
		return handleError(c, core.NewProviderError("routing_state", http.StatusBadGateway, "failed to delete routing state", err))
	}
	return c.NoContent(http.StatusNoContent)
}
