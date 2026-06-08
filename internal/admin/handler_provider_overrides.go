package admin

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"gomodel/internal/core"
	"gomodel/internal/provideroverrides"
)

// upsertProviderOverrideRequest represents the request body for creating/updating a provider override.
type upsertProviderOverrideRequest struct {
	ProviderName string `json:"provider_name"`
	Enabled      bool   `json:"enabled"`
}

// deleteProviderOverrideRequest represents the request body for deleting a provider override.
type deleteProviderOverrideRequest struct {
	ProviderName string `json:"provider_name"`
}

// ListProviderOverrides handles GET /admin/provider-overrides.
//
// @Summary      List provider overrides
// @Description  Lists all provider enable/disable overrides
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   provideroverrides.View
// @Failure      401  {object}  core.GatewayError
// @Failure      503  {object}  core.GatewayError
// @Router       /admin/provider-overrides [get]
func (h *Handler) ListProviderOverrides(c *echo.Context) error {
	if h.providerOverrides == nil {
		return handleError(c, featureUnavailableError("provider overrides feature is unavailable"))
	}
	views := h.providerOverrides.ListViews()
	if views == nil {
		views = []provideroverrides.View{}
	}
	return c.JSON(http.StatusOK, views)
}

// UpsertProviderOverride handles PUT /admin/provider-overrides.
//
// @Summary      Create or update one provider override
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        override  body      upsertProviderOverrideRequest  true  "Provider name and enabled state"
// @Success      200       {object}  provideroverrides.View
// @Failure      400       {object}  core.GatewayError
// @Failure      401       {object}  core.GatewayError
// @Failure      500       {object}  core.GatewayError
// @Failure      502       {object}  core.GatewayError
// @Failure      503       {object}  core.GatewayError
// @Router       /admin/provider-overrides [put]
func (h *Handler) UpsertProviderOverride(c *echo.Context) error {
	if h.providerOverrides == nil {
		return handleError(c, featureUnavailableError("provider overrides feature is unavailable"))
	}

	var req upsertProviderOverrideRequest
	if err := c.Bind(&req); err != nil {
		return handleError(c, core.NewInvalidRequestError("invalid request body: "+err.Error(), err))
	}

	// Normalize provider name
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		return handleError(c, core.NewInvalidRequestError("provider_name is required", nil))
	}

	if err := h.providerOverrides.Upsert(c.Request().Context(), provideroverrides.ProviderOverride{
		ProviderName: providerName,
		Enabled:      req.Enabled,
	}); err != nil {
		return handleError(c, providerOverrideWriteError(err))
	}

	override, ok := h.providerOverrides.Get(providerName)
	if !ok || override == nil {
		return handleError(c, core.NewProviderError("provider_overrides", http.StatusInternalServerError, "provider override update failed unexpectedly", nil))
	}
	return c.JSON(http.StatusOK, provideroverrides.NewView(*override))
}

// GetProviderOverride handles GET /admin/provider-overrides/{name}.
//
// @Summary      Get one provider override
// @Description  Retrieves a specific provider override by name
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Param        name  path      string  true  "Provider name"
// @Success      200   {object}  provideroverrides.View
// @Failure      400   {object}  core.GatewayError
// @Failure      401   {object}  core.GatewayError
// @Failure      404   {object}  core.GatewayError
// @Router       /admin/provider-overrides/{name} [get]
func (h *Handler) GetProviderOverride(c *echo.Context) error {
	if h.providerOverrides == nil {
		return handleError(c, featureUnavailableError("provider overrides feature is unavailable"))
	}

	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		return handleError(c, core.NewInvalidRequestError("provider name is required", nil))
	}

	override, ok := h.providerOverrides.Get(name)
	if !ok || override == nil {
		return handleError(c, core.NewNotFoundError("provider override not found: "+name))
	}
	return c.JSON(http.StatusOK, provideroverrides.NewView(*override))
}

// DeleteProviderOverride handles DELETE /admin/provider-overrides/{name}.
//
// @Summary      Delete one provider override
// @Description  Removes a provider override, reverting to default behavior
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Param        name  path      string  true  "Provider name"
// @Success      204   "No Content"
// @Failure      400   {object}  core.GatewayError
// @Failure      401   {object}  core.GatewayError
// @Failure      404   {object}  core.GatewayError
// @Router       /admin/provider-overrides/{name} [delete]
func (h *Handler) DeleteProviderOverride(c *echo.Context) error {
	if h.providerOverrides == nil {
		return handleError(c, featureUnavailableError("provider overrides feature is unavailable"))
	}

	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		return handleError(c, core.NewInvalidRequestError("provider name is required", nil))
	}

	if err := h.providerOverrides.Delete(c.Request().Context(), name); err != nil {
		return handleError(c, providerOverrideWriteError(err))
	}
	return c.NoContent(http.StatusNoContent)
}

// providerOverrideWriteError maps store errors to appropriate HTTP responses.
func providerOverrideWriteError(err error) error {
	if err == nil {
		return nil
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "provider_name is required") {
		return core.NewInvalidRequestError(errMsg, err)
	}
	if strings.Contains(errMsg, "provider does not exist") {
		return core.NewNotFoundError(errMsg)
	}
	if strings.Contains(errMsg, "not found") {
		return core.NewNotFoundError(errMsg)
	}
	return core.NewProviderError("provider_overrides", http.StatusInternalServerError, errMsg, err)
}
