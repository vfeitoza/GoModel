package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"gomodel/internal/oauth"
	"gomodel/internal/oauthstore"
	"gomodel/internal/oauthusage"
	"gomodel/internal/providers"
)

// OAuthProviderStatus is the admin-facing view of a single OAuth provider.
type OAuthProviderStatus struct {
	ProviderName     string     `json:"provider_name"`
	ProviderType     string     `json:"provider_type"`
	Status           string     `json:"status"` // "pending", "authenticated", "expired", "error"
	Authenticated    bool       `json:"authenticated"`
	AccountEmail     string     `json:"account_email,omitempty"`
	DisplayName      string     `json:"display_name,omitempty"`
	SubscriptionType string     `json:"subscription_type,omitempty"`
	TokenExpiresAt   *time.Time `json:"token_expires_at,omitempty"`
	LastRefreshedAt  *time.Time `json:"last_refreshed_at,omitempty"`
}

// OAuthUsageWindowResponse is the admin-facing view of a usage window.
type OAuthUsageWindowResponse struct {
	Utilization        float64   `json:"utilization"`
	UtilizationPercent int       `json:"utilization_percent"`
	ResetsAt           time.Time `json:"resets_at"`
}

// OAuthExtraUsageResponse is the admin-facing view of extra credit usage.
type OAuthExtraUsageResponse struct {
	IsEnabled      bool    `json:"is_enabled"`
	MonthlyLimit   float64 `json:"monthly_limit,omitempty"`
	UsedCredits    float64 `json:"used_credits,omitempty"`
	Utilization    float64 `json:"utilization,omitempty"`
	DisabledReason string  `json:"disabled_reason,omitempty"`
}

// OAuthUsageResponse is the admin-facing view of OAuth usage data.
type OAuthUsageResponse struct {
	ProviderName      string                     `json:"provider_name"`
	AccountEmail      string                     `json:"account_email"`
	SubscriptionType  string                     `json:"subscription_type"`
	FiveHour          *OAuthUsageWindowResponse  `json:"five_hour,omitempty"`
	SevenDay          *OAuthUsageWindowResponse  `json:"seven_day,omitempty"`
	SevenDayOAuthApps *OAuthUsageWindowResponse  `json:"seven_day_oauth_apps,omitempty"`
	SevenDayOpus      *OAuthUsageWindowResponse  `json:"seven_day_opus,omitempty"`
	SevenDaySonnet    *OAuthUsageWindowResponse  `json:"seven_day_sonnet,omitempty"`
	ExtraUsage        *OAuthExtraUsageResponse   `json:"extra_usage,omitempty"`
	FetchedAt         time.Time                  `json:"fetched_at"`
}

// oauthFlowState holds in-progress PKCE state for a pending OAuth flow.
type oauthFlowState struct {
	verifier     string
	state        string
	providerName string
	providerType string
	callbackPort int
	server       *oauth.CallbackServer
	createdAt    time.Time
}

// OAuthHandler handles OAuth-related admin endpoints.
type OAuthHandler struct {
	store              oauthstore.Store
	usageFetcher       *oauthusage.CachingFetcher
	configuredProviders []providers.SanitizedProviderConfig

	flowMu  sync.Mutex
	flows   map[string]*oauthFlowState // keyed by state token
}

// NewOAuthHandler creates a new OAuthHandler.
func NewOAuthHandler(store oauthstore.Store, configuredProviders []providers.SanitizedProviderConfig) *OAuthHandler {
	return &OAuthHandler{
		store:               store,
		usageFetcher:        oauthusage.NewCachingFetcher(oauthusage.NewHTTPFetcher()),
		configuredProviders: configuredProviders,
		flows:               make(map[string]*oauthFlowState),
	}
}

// RegisterOAuthRoutes mounts the OAuth admin routes on the given registrar.
func (h *OAuthHandler) RegisterOAuthRoutes(g RouteRegistrar) {
	g.GET("/oauth/providers", h.ListOAuthProviders)
	g.POST("/oauth/start", h.StartOAuth)
	g.GET("/oauth/callback", h.OAuthCallback)
	g.POST("/oauth/revoke", h.RevokeOAuth)
	g.GET("/oauth/usage/:provider_name", h.GetOAuthUsage)
	g.GET("/oauth/status/:provider_name", h.GetOAuthStatus)
}

// ListOAuthProviders returns all providers configured with api_key: "oauth".
func (h *OAuthHandler) ListOAuthProviders(c *echo.Context) error {
	ctx := c.Request().Context()
	statuses, err := h.buildProviderStatuses(ctx)
	if err != nil {
		return handleError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{
		"providers": statuses,
		"total":     len(statuses),
	})
}

// StartOAuth initiates the OAuth flow for a provider.
// Body: {"provider_name": "anthropic_oauth"}
func (h *OAuthHandler) StartOAuth(c *echo.Context) error {
	var req struct {
		ProviderName string `json:"provider_name"`
	}
	if err := c.Bind(&req); err != nil {
		return handleError(c, fmt.Errorf("invalid request body: %w", err))
	}
	req.ProviderName = strings.TrimSpace(req.ProviderName)
	if req.ProviderName == "" {
		return handleError(c, fmt.Errorf("provider_name is required"))
	}

	// Verify the provider exists and is OAuth-configured
	provCfg, ok := h.findOAuthProvider(req.ProviderName)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": fmt.Sprintf("provider %q not found or not configured for OAuth", req.ProviderName),
		})
	}

	// Generate PKCE pair and state
	pkce, err := oauth.NewPKCEPair()
	if err != nil {
		return handleError(c, fmt.Errorf("generate PKCE: %w", err))
	}
	state, err := oauth.NewState()
	if err != nil {
		return handleError(c, fmt.Errorf("generate state: %w", err))
	}

	// Start callback server
	fallbackPorts := []int{54546, 54547, 54548, 54549, 54550}
	cs, actualPort, err := oauth.TryCallbackPorts(oauth.DefaultCallbackPort, fallbackPorts...)
	if err != nil {
		return handleError(c, fmt.Errorf("start OAuth callback server: %w", err))
	}

	// Store flow state
	h.flowMu.Lock()
	h.cleanExpiredFlows()
	h.flows[state] = &oauthFlowState{
		verifier:     pkce.Verifier,
		state:        state,
		providerName: req.ProviderName,
		providerType: provCfg.Type,
		callbackPort: actualPort,
		server:       cs,
		createdAt:    time.Now(),
	}
	h.flowMu.Unlock()

	// Build authorization URL
	oauthProv := oauth.NewAnthropicProvider()
	authURL := oauthProv.AuthorizationURL(state, pkce.Verifier, actualPort)

	// Wait for callback in background
	go h.waitForCallback(state)

	return c.JSON(http.StatusOK, map[string]any{
		"auth_url":      authURL,
		"state":         state,
		"callback_port": actualPort,
	})
}

// OAuthCallback handles the redirect from the OAuth provider.
func (h *OAuthHandler) OAuthCallback(c *echo.Context) error {
	code := c.QueryParam("code")
	state := c.QueryParam("state")
	errParam := c.QueryParam("error")

	if errParam != "" {
		return c.HTML(http.StatusBadRequest, oauthErrorHTML(errParam))
	}
	if code == "" || state == "" {
		return c.HTML(http.StatusBadRequest, oauthErrorHTML("missing code or state parameter"))
	}

	h.flowMu.Lock()
	flow, ok := h.flows[state]
	h.flowMu.Unlock()

	if !ok {
		return c.HTML(http.StatusBadRequest, oauthErrorHTML("invalid or expired OAuth state — please try again"))
	}

	ctx := c.Request().Context()
	if err := h.completeOAuthFlow(ctx, flow, code, state); err != nil {
		slog.Error("oauth callback: flow completion failed", "provider", flow.providerName, "error", err)
		return c.HTML(http.StatusInternalServerError, oauthErrorHTML("authentication failed: "+err.Error()))
	}

	h.flowMu.Lock()
	delete(h.flows, state)
	h.flowMu.Unlock()

	return c.HTML(http.StatusOK, oauthSuccessHTML())
}

// RevokeOAuth removes the stored OAuth token for a provider.
// Body: {"provider_name": "anthropic_oauth"}
func (h *OAuthHandler) RevokeOAuth(c *echo.Context) error {
	var req struct {
		ProviderName string `json:"provider_name"`
	}
	if err := c.Bind(&req); err != nil {
		return handleError(c, fmt.Errorf("invalid request body: %w", err))
	}
	req.ProviderName = strings.TrimSpace(req.ProviderName)
	if req.ProviderName == "" {
		return handleError(c, fmt.Errorf("provider_name is required"))
	}

	ctx := c.Request().Context()
	if err := h.store.Delete(ctx, req.ProviderName); err != nil {
		return handleError(c, fmt.Errorf("revoke OAuth token: %w", err))
	}

	if h.usageFetcher != nil {
		h.usageFetcher.Invalidate(req.ProviderName)
	}

	slog.Info("oauth: token revoked", "provider", req.ProviderName)
	return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

// GetOAuthUsage returns usage data for an OAuth provider.
func (h *OAuthHandler) GetOAuthUsage(c *echo.Context) error {
	providerName := strings.TrimSpace(c.Param("provider_name"))
	if providerName == "" {
		return handleError(c, fmt.Errorf("provider_name is required"))
	}

	ctx := c.Request().Context()
	token, err := h.store.Get(ctx, providerName)
	if err != nil {
		if errors.Is(err, oauthstore.ErrNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{
				"error": fmt.Sprintf("provider %q is not authenticated", providerName),
			})
		}
		return handleError(c, err)
	}

	usage, err := h.usageFetcher.FetchUsage(ctx, providerName, token.AccessToken)
	if err != nil {
		return handleError(c, fmt.Errorf("fetch OAuth usage: %w", err))
	}
	if usage == nil {
		return c.JSON(http.StatusOK, map[string]any{
			"provider_name": providerName,
			"account_email": token.AccountEmail,
			"note":          "usage data not available for this account",
		})
	}

	return c.JSON(http.StatusOK, buildUsageResponse(providerName, token, usage))
}

// GetOAuthStatus returns the authentication status for a single OAuth provider.
func (h *OAuthHandler) GetOAuthStatus(c *echo.Context) error {
	providerName := strings.TrimSpace(c.Param("provider_name"))
	if providerName == "" {
		return handleError(c, fmt.Errorf("provider_name is required"))
	}

	ctx := c.Request().Context()
	status := h.buildProviderStatus(ctx, providerName, "")
	return c.JSON(http.StatusOK, status)
}

// --- helpers ---

func (h *OAuthHandler) buildProviderStatuses(ctx context.Context) ([]OAuthProviderStatus, error) {
	result := make([]OAuthProviderStatus, 0)
	for _, p := range h.configuredProviders {
		if !isOAuthProviderConfig(p) {
			continue
		}
		result = append(result, h.buildProviderStatus(ctx, p.Name, p.Type))
	}
	return result, nil
}

func (h *OAuthHandler) buildProviderStatus(ctx context.Context, providerName, providerType string) OAuthProviderStatus {
	status := OAuthProviderStatus{
		ProviderName: providerName,
		ProviderType: providerType,
		Status:       "pending",
	}

	token, err := h.store.Get(ctx, providerName)
	if err != nil {
		if !errors.Is(err, oauthstore.ErrNotFound) {
			slog.Warn("oauth: failed to load token for status", "provider", providerName, "error", err)
		}
		return status
	}

	if providerType == "" {
		status.ProviderType = token.ProviderType
	}
	status.AccountEmail = token.AccountEmail
	status.DisplayName = token.DisplayName
	status.SubscriptionType = token.SubscriptionType
	expiresAt := token.ExpiresAt
	status.TokenExpiresAt = &expiresAt
	updatedAt := token.UpdatedAt
	status.LastRefreshedAt = &updatedAt

	if token.IsExpired() {
		status.Status = "expired"
		status.Authenticated = false
	} else {
		status.Status = "authenticated"
		status.Authenticated = true
	}

	return status
}

func (h *OAuthHandler) findOAuthProvider(name string) (providers.SanitizedProviderConfig, bool) {
	for _, p := range h.configuredProviders {
		if p.Name == name && isOAuthProviderConfig(p) {
			return p, true
		}
	}
	return providers.SanitizedProviderConfig{}, false
}

func isOAuthProviderConfig(p providers.SanitizedProviderConfig) bool {
	return p.IsOAuth
}

func (h *OAuthHandler) waitForCallback(state string) {
	h.flowMu.Lock()
	flow, ok := h.flows[state]
	h.flowMu.Unlock()
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, err := flow.server.Wait(ctx)
	if err != nil {
		slog.Warn("oauth: callback wait failed", "provider", flow.providerName, "error", err)
		h.flowMu.Lock()
		delete(h.flows, state)
		h.flowMu.Unlock()
		return
	}

	if err := h.completeOAuthFlow(ctx, flow, result.Code, result.State); err != nil {
		slog.Error("oauth: flow completion failed (background)", "provider", flow.providerName, "error", err)
	}

	h.flowMu.Lock()
	delete(h.flows, state)
	h.flowMu.Unlock()
}

func (h *OAuthHandler) completeOAuthFlow(ctx context.Context, flow *oauthFlowState, code, state string) error {
	oauthProv := oauth.NewAnthropicProvider()

	tokens, err := oauthProv.ExchangeCode(ctx, code, flow.verifier, state, flow.callbackPort)
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}

	profile, err := oauthProv.FetchProfile(ctx, tokens.AccessToken)
	if err != nil {
		slog.Warn("oauth: profile fetch failed, continuing without profile", "provider", flow.providerName, "error", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	if tokens.ExpiresIn <= 0 {
		expiresAt = time.Now().Add(24 * time.Hour) // safe default
	}

	token := &oauthstore.Token{
		ProviderName:     flow.providerName,
		ProviderType:     flow.providerType,
		AccessToken:      tokens.AccessToken,
		RefreshToken:     tokens.RefreshToken,
		ExpiresAt:        expiresAt,
		Scopes:           tokens.Scopes,
		SubscriptionType: tokens.SubscriptionType,
	}
	if profile != nil {
		token.AccountEmail = profile.Email
		token.AccountID = profile.AccountID
		token.DisplayName = profile.DisplayName
		if profile.SubscriptionType != "" {
			token.SubscriptionType = profile.SubscriptionType
		}
	}

	if err := h.store.Save(ctx, token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	slog.Info("oauth: authentication successful",
		"provider", flow.providerName,
		"email", token.AccountEmail,
		"subscription", token.SubscriptionType,
	)
	return nil
}

func (h *OAuthHandler) cleanExpiredFlows() {
	cutoff := time.Now().Add(-5 * time.Minute)
	for state, flow := range h.flows {
		if flow.createdAt.Before(cutoff) {
			delete(h.flows, state)
		}
	}
}

func buildUsageResponse(providerName string, token *oauthstore.Token, usage *oauthusage.Usage) OAuthUsageResponse {
	resp := OAuthUsageResponse{
		ProviderName:     providerName,
		AccountEmail:     token.AccountEmail,
		SubscriptionType: token.SubscriptionType,
		FetchedAt:        usage.FetchedAt,
	}
	resp.FiveHour = toWindowResponse(usage.FiveHour)
	resp.SevenDay = toWindowResponse(usage.SevenDay)
	resp.SevenDayOAuthApps = toWindowResponse(usage.SevenDayOAuthApps)
	resp.SevenDayOpus = toWindowResponse(usage.SevenDayOpus)
	resp.SevenDaySonnet = toWindowResponse(usage.SevenDaySonnet)
	if usage.ExtraUsage != nil {
		resp.ExtraUsage = &OAuthExtraUsageResponse{
			IsEnabled:      usage.ExtraUsage.IsEnabled,
			MonthlyLimit:   usage.ExtraUsage.MonthlyLimit,
			UsedCredits:    usage.ExtraUsage.UsedCredits,
			Utilization:    usage.ExtraUsage.Utilization,
			DisabledReason: usage.ExtraUsage.DisabledReason,
		}
	}
	return resp
}

func toWindowResponse(w *oauthusage.UsageWindow) *OAuthUsageWindowResponse {
	if w == nil {
		return nil
	}
	return &OAuthUsageWindowResponse{
		Utilization:        w.Utilization,
		UtilizationPercent: w.UtilizationPercent(),
		ResetsAt:           w.ResetsAt,
	}
}

func oauthSuccessHTML() string {
	return `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Authentication Successful</title>
<style>
body{font-family:system-ui,sans-serif;background:#0f172a;color:#f8fafc;
     display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{background:#1e293b;padding:3rem;border-radius:1rem;text-align:center;
      max-width:400px;border:1px solid #334155}
h1{color:#a78bfa;margin:0 0 1rem}p{color:#94a3b8}
</style></head>
<body>
<div class="card">
  <h1>&#10003; Authentication Successful</h1>
  <p>You can close this window and return to the dashboard.</p>
</div>
<script>
if(window.opener)window.opener.postMessage({type:'gomodel-oauth-success'},'*');
setTimeout(()=>window.close(),3000);
</script>
</body></html>`
}

func oauthErrorHTML(errMsg string) string {
	return `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Authentication Failed</title>
<style>
body{font-family:system-ui,sans-serif;background:#0f172a;color:#f8fafc;
     display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{background:#1e293b;padding:3rem;border-radius:1rem;text-align:center;
      max-width:400px;border:1px solid #334155}
h1{color:#ef4444;margin:0 0 1rem}p{color:#94a3b8}
.err{background:rgba(239,68,68,.1);padding:1rem;border-radius:.5rem;
     color:#fca5a5;font-family:monospace;font-size:.9rem;margin-top:1rem}
</style></head>
<body>
<div class="card">
  <h1>&#10007; Authentication Failed</h1>
  <p>Please close this window and try again.</p>
  <div class="err">` + errMsg + `</div>
</div>
</body></html>`
}
