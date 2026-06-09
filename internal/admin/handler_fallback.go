package admin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/labstack/echo/v5"

	"gomodel/internal/core"
)

// FallbackRule represents a single fallback rule mapping a source model to fallback models.
type FallbackRule struct {
	SourceModel    string   `json:"source_model"`
	FallbackModels []string `json:"fallback_models"`
}

// FallbackRulesResponse wraps the list of fallback rules.
type FallbackRulesResponse struct {
	Rules []FallbackRule `json:"rules"`
}

// GetFallbackRules handles GET /admin/fallback/rules.
//
// @Summary      List fallback rules
// @Description  Lists all configured fallback rules from fallback.json
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  FallbackRulesResponse
// @Failure      401  {object}  core.GatewayError
// @Failure      500  {object}  core.GatewayError
// @Router       /admin/fallback/rules [get]
func (h *Handler) GetFallbackRules(c *echo.Context) error {
	path := h.fallbackManualRulesPath()
	if path == "" {
		return c.JSON(http.StatusOK, FallbackRulesResponse{Rules: []FallbackRule{}})
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusOK, FallbackRulesResponse{Rules: []FallbackRule{}})
		}
		return handleError(c, core.NewProviderError("fallback", http.StatusInternalServerError, "failed to read fallback rules: "+err.Error(), err))
	}

	var rulesMap map[string][]string
	if err := json.Unmarshal(raw, &rulesMap); err != nil {
		return handleError(c, core.NewProviderError("fallback", http.StatusInternalServerError, "failed to parse fallback rules: "+err.Error(), err))
	}

	rules := make([]FallbackRule, 0, len(rulesMap))
	for sourceModel, fallbackModels := range rulesMap {
		rules = append(rules, FallbackRule{
			SourceModel:    sourceModel,
			FallbackModels: fallbackModels,
		})
	}

	// Sort by source model for consistent ordering
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].SourceModel < rules[j].SourceModel
	})

	return c.JSON(http.StatusOK, FallbackRulesResponse{Rules: rules})
}

// UpsertFallbackRuleRequest represents the request body for creating/updating a fallback rule.
type UpsertFallbackRuleRequest struct {
	SourceModel    string   `json:"source_model"`
	FallbackModels []string `json:"fallback_models"`
}

// UpsertFallbackRule handles PUT /admin/fallback/rules.
//
// @Summary      Create or update one fallback rule
// @Description  Creates or updates a fallback rule for a source model
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        rule  body      UpsertFallbackRuleRequest  true  "Fallback rule"
// @Success      200   {object}  FallbackRule
// @Failure      400   {object}  core.GatewayError
// @Failure      401   {object}  core.GatewayError
// @Failure      500   {object}  core.GatewayError
// @Router       /admin/fallback/rules [put]
func (h *Handler) UpsertFallbackRule(c *echo.Context) error {
	var req UpsertFallbackRuleRequest
	if err := c.Bind(&req); err != nil {
		return handleError(c, core.NewInvalidRequestError("invalid request body: "+err.Error(), err))
	}

	sourceModel := strings.TrimSpace(req.SourceModel)
	if sourceModel == "" {
		return handleError(c, core.NewInvalidRequestError("source_model is required", nil))
	}

	// Normalize fallback models
	normalized := make([]string, 0, len(req.FallbackModels))
	for _, model := range req.FallbackModels {
		model = strings.TrimSpace(model)
		if model != "" {
			normalized = append(normalized, model)
		}
	}

	path := h.fallbackManualRulesPath()
	if path == "" {
		return handleError(c, core.NewProviderError("fallback", http.StatusServiceUnavailable, "fallback manual rules path not configured", nil))
	}

	// Read existing rules
	rulesMap := make(map[string][]string)
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return handleError(c, core.NewProviderError("fallback", http.StatusInternalServerError, "failed to read fallback rules: "+err.Error(), err))
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &rulesMap); err != nil {
			return handleError(c, core.NewProviderError("fallback", http.StatusInternalServerError, "failed to parse fallback rules: "+err.Error(), err))
		}
	}

	// Update or add the rule
	rulesMap[sourceModel] = normalized

	// Write back
	if err := h.writeFallbackRules(path, rulesMap); err != nil {
		return handleError(c, core.NewProviderError("fallback", http.StatusInternalServerError, "failed to write fallback rules: "+err.Error(), err))
	}

	return c.JSON(http.StatusOK, FallbackRule{
		SourceModel:    sourceModel,
		FallbackModels: normalized,
	})
}

// DeleteFallbackRule handles DELETE /admin/fallback/rules/:sourceModel.
//
// @Summary      Delete one fallback rule
// @Description  Removes a fallback rule for a source model
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Param        sourceModel  path  string  true  "Source model identifier"
// @Success      204          "No Content"
// @Failure      400          {object}  core.GatewayError
// @Failure      401          {object}  core.GatewayError
// @Failure      404          {object}  core.GatewayError
// @Failure      500          {object}  core.GatewayError
// @Router       /admin/fallback/rules/{sourceModel} [delete]
func (h *Handler) DeleteFallbackRule(c *echo.Context) error {
	sourceModel := strings.TrimSpace(c.Param("sourceModel"))
	if sourceModel == "" {
		return handleError(c, core.NewInvalidRequestError("source_model is required", nil))
	}

	path := h.fallbackManualRulesPath()
	if path == "" {
		return handleError(c, core.NewProviderError("fallback", http.StatusServiceUnavailable, "fallback manual rules path not configured", nil))
	}

	// Read existing rules
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return handleError(c, core.NewNotFoundError("fallback rule not found: "+sourceModel))
		}
		return handleError(c, core.NewProviderError("fallback", http.StatusInternalServerError, "failed to read fallback rules: "+err.Error(), err))
	}

	var rulesMap map[string][]string
	if err := json.Unmarshal(raw, &rulesMap); err != nil {
		return handleError(c, core.NewProviderError("fallback", http.StatusInternalServerError, "failed to parse fallback rules: "+err.Error(), err))
	}

	if _, exists := rulesMap[sourceModel]; !exists {
		return handleError(c, core.NewNotFoundError("fallback rule not found: "+sourceModel))
	}

	// Remove the rule
	delete(rulesMap, sourceModel)

	// Write back
	if err := h.writeFallbackRules(path, rulesMap); err != nil {
		return handleError(c, core.NewProviderError("fallback", http.StatusInternalServerError, "failed to write fallback rules: "+err.Error(), err))
	}

	return c.NoContent(http.StatusNoContent)
}

// fallbackManualRulesPath returns the configured fallback manual rules path from config.
func (h *Handler) fallbackManualRulesPath() string {
	if h.cfg == nil {
		return ""
	}
	return strings.TrimSpace(h.cfg.Fallback.ManualRulesPath)
}

// writeFallbackRules writes the fallback rules map to the specified path as formatted JSON.
func (h *Handler) writeFallbackRules(path string, rulesMap map[string][]string) error {
	// Get absolute path for logging
	absPath, _ := filepath.Abs(path)
	slog.Info("writing fallback rules", "path", path, "absolute_path", absPath)

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(rulesMap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}

	// Write atomically using temp file + rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // cleanup on failure
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}
