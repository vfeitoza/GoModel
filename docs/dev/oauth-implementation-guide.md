# OAuth Provider Implementation Guide

Reference implementation: Anthropic OAuth (branch `feat/anthropic-oauth-pkce`)

This document captures every decision, file, and fix made when adding OAuth 2.0 + PKCE support for the Anthropic provider. Use it as the blueprint when adding OAuth for other providers (e.g. OpenAI Codex).

---

## Overview

OAuth providers are configured with `api_key: "oauth"` in `config.yaml`. Once the user authenticates via the admin dashboard, the provider behaves identically to a static API key provider — tokens are stored, refreshed automatically, and injected into upstream requests.

```yaml
providers:
  my_claude:
    type: anthropic
    api_key: "oauth"
```

Requests are sent via the passthrough route:

```
POST /p/{provider_name}/v1/chat/completions
```

---

## Dual-mode callback — no configuration required

GoModel supports both local and remote OAuth flows without any extra configuration:

| Mode | When to use | How it works |
|---|---|---|
| **Local** (Authenticate button) | GoModel and browser on the same machine | Popup redirects to `http://localhost:54545/callback` — GoModel receives the code automatically |
| **Remote** (Remote button) | GoModel on a remote server | Popup redirects to `https://platform.claude.com/oauth/code/callback` — user copies the URL and pastes it in the dashboard |

Both modes are always available. No `GOMODEL_PUBLIC_URL` or any other config needed.

---

## Architecture

```
config.yaml (api_key: "oauth")
    │
    ▼
ProviderFactory.SetOAuthStore(store)   ← called in app.go before providers.Init()
    │
    ▼
AnthropicProvider detects "oauth" sentinel
    │
    ├── on request: load token from store, inject as Bearer
    ├── on expiry:  call RefreshToken(), persist new token
    └── on missing token: cancel request context → upstream call aborted
```

### New packages

| Package | Path | Purpose |
|---|---|---|
| `oauth` | `internal/oauth/` | OAuth 2.0 + PKCE primitives, provider interface, Anthropic implementation |
| `oauthstore` | `internal/oauthstore/` | Token persistence (SQLite, PostgreSQL, MongoDB) |
| `oauthusage` | `internal/oauthusage/` | Fetch and cache Anthropic rate-limit usage windows |

---

## Package: `internal/oauth`

### `oauth.go` — core types and PKCE helpers

```go
type Provider interface {
    // redirectURI is the full callback URI — either LocalCallbackURI(port)
    // or a provider-hosted URI like platform.claude.com/oauth/code/callback.
    AuthorizationURL(state, verifier, redirectURI string) string
    ExchangeCode(ctx context.Context, code, verifier, state, redirectURI string) (*TokenResponse, error)
    RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)
    FetchProfile(ctx context.Context, accessToken string) (*Profile, error)
}
```

Key functions:
- `NewPKCEPair()` — generates verifier + S256 challenge
- `NewState()` — generates hex-encoded CSRF state (16 bytes → 32 hex chars)
- `LocalCallbackURI(port)` — builds `http://localhost:{port}/callback`

**Critical**: State must be hex-encoded (`hex.EncodeToString`), not base64url. Anthropic rejects base64url states with "Invalid request format".

### `anthropic.go` — Anthropic provider

Constants:
```go
AnthropicClientID   = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"  // same as Claude Code
anthropicAuthURL    = "https://claude.ai/oauth/authorize"
anthropicTokenURL   = "https://console.anthropic.com/v1/oauth/token"
anthropicProfileURL = "https://api.anthropic.com/api/oauth/profile"
DefaultCallbackPort = 54545
anthropicDefaultScopes = "org:create_api_key user:profile user:inference"
```

**Critical quirks**:
1. `code=true` must be the **first** parameter in the authorization URL
2. Query string must be built **manually** (not via `url.Values.Encode()` which sorts alphabetically)
3. Redirect URI must use `http://localhost:{port}/callback` (not `127.0.0.1`)
4. `state` must be included in the token exchange body (non-standard)
5. For remote/manual flow, use `https://platform.claude.com/oauth/code/callback` as redirect URI — `console.anthropic.com/oauth/code/callback` redirects there, and the token exchange must use the **final** URI

Authorization URL construction:
```go
query := "code=true" +
    "&client_id=" + url.QueryEscape(AnthropicClientID) +
    "&redirect_uri=" + url.QueryEscape(redirectURI) +
    "&response_type=code" +
    "&scope=" + url.QueryEscape(anthropicDefaultScopes) +
    "&state=" + url.QueryEscape(state) +
    "&code_challenge=" + url.QueryEscape(challenge) +
    "&code_challenge_method=S256"
```

Reference implementation: `/Users/vfeitoza/Projetos/cligate/src/claude-oauth.js`

---

## Package: `internal/oauthstore`

### Store interface

```go
type Store interface {
    Save(ctx context.Context, token *Token) error
    Get(ctx context.Context, providerName string) (*Token, error)
    Delete(ctx context.Context, providerName string) error
    List(ctx context.Context) ([]*Token, error)
    Close() error
}
```

### Token struct

```go
type Token struct {
    ProviderName     string
    AccessToken      string
    RefreshToken     string
    ExpiresAt        time.Time
    Scopes           []string
    AccountID        string
    AccountEmail     string
    DisplayName      string
    SubscriptionType string
}
```

### Factory

```go
// internal/oauthstore/factory.go
func NewFromStorage(ctx context.Context, shared storage.Storage) (Store, error) {
    return storage.ResolveBackend[Store](
        shared,
        func(db *sql.DB) (Store, error) { return NewSQLiteStore(db) },
        func(pool *pgxpool.Pool) (Store, error) { return NewPostgreSQLStore(ctx, pool) },
        func(db *mongo.Database) (Store, error) { return NewMongoDBStore(db) },
    )
}
```

Follows the same pattern as `internal/authkeys/factory.go`.

---

## Admin API endpoints

Registered under `/admin/api/v1/oauth` via `admin.RegisterOAuthRoutes(group, handler)`.

| Method | Path | Description |
|---|---|---|
| GET | `/oauth/providers` | List all OAuth-configured providers with status |
| POST | `/oauth/start` | Start PKCE flow, returns `auth_url`, `manual_auth_url`, `state` |
| GET | `/oauth/callback` | Receive authorization code from local callback server |
| POST | `/oauth/callback-manual` | Receive pasted callback URL or raw code from dashboard |
| POST | `/oauth/revoke` | Delete stored token |
| GET | `/oauth/usage/:name` | Fetch usage windows for a provider |
| GET | `/oauth/status/:name` | Token status for a single provider |

### `StartOAuth` response

```json
{
  "auth_url": "https://claude.ai/oauth/authorize?...&redirect_uri=http%3A%2F%2Flocalhost%3A54545%2Fcallback...",
  "manual_auth_url": "https://claude.ai/oauth/authorize?...&redirect_uri=https%3A%2F%2Fplatform.claude.com%2Foauth%2Fcode%2Fcallback...",
  "manual_uri": "https://platform.claude.com/oauth/code/callback",
  "state": "4b477a04aea23843fb82c61e0872cb31",
  "callback_port": 54545
}
```

### `oauthFlowState` — dual redirect URI

```go
type oauthFlowState struct {
    verifier     string
    state        string
    providerName string
    providerType string
    redirectURI  string            // used in AuthorizationURL (local: localhost, manual: platform.claude.com)
    exchangeURI  string            // used in token exchange (same as redirectURI for local; platform.claude.com for manual)
    callbackPort int
    server       *oauth.CallbackServer
    createdAt    time.Time
}
```

### Handler wiring (`internal/app/app.go`)

```go
// 1. Create store before providers.Init()
oauthStore, err := oauthstore.NewFromStorage(ctx, sharedStorage)

// 2. Pass store to provider factory
cfg.Factory.SetOAuthStore(oauthStore)

// 3. Create handler
oauthHandler = admin.NewOAuthHandler(oauthStore, configuredProviders)

// 4. Wire into server config
serverCfg.OAuthHandler = oauthHandler
```

### Route registration (`internal/server/http.go`)

```go
if cfg != nil && cfg.AdminEndpointsEnabled && cfg.AdminHandler != nil {
    adminGroup := e.Group("/admin/api/v1")
    cfg.AdminHandler.RegisterRoutes(adminGroup)
    admin.RegisterOAuthRoutes(adminGroup, cfg.OAuthHandler)
}
```

**Note**: Echo v5 uses `c.Param()`, not `c.PathParam()`.

---

## Dashboard UI

### Files

| File | Change |
|---|---|
| `internal/admin/dashboard/templates/page-oauth.html` | OAuth page template |
| `internal/admin/dashboard/templates/index.html` | Added `{{template "dashboard-page-oauth" .}}` |
| `internal/admin/dashboard/templates/layout.html` | Added `<script src="...oauth.js">` |
| `internal/admin/dashboard/static/js/modules/oauth.js` | OAuth Alpine.js module |
| `internal/admin/dashboard/static/js/dashboard.js` | Registered OAuth page and module |

### Module pattern

The module must follow the **IIFE global pattern** used by all other dashboard modules:

```js
(function(global) {
    function dashboardOAuthModule() {
        return {
            // state and methods
        };
    }
    global.dashboardOAuthModule = dashboardOAuthModule;
})(window);
```

**Do not** use ES module syntax (`export function`). The dashboard uses a module factory system that expects globals on `window`.

### Registering in `dashboard.js`

1. Add page name to the allowlist in `_parseRoute()`
2. Add init hook in `_applyRoute()`: `if (page === "oauth" && typeof this.oauthInit === "function") { this.oauthInit(); }`
3. Add to `moduleFactories` array

### Alpine.js reactivity — critical pattern

**Problem**: Alpine v3 does not track mutations of nested object properties.

**Wrong**:
```js
oauthAuthenticating: {},  // Alpine won't track [key] changes
:disabled="oauthAuthenticating[provider.provider_name]"
```

**Correct**: Use string primitives for single-active-at-a-time state:
```js
oauthActiveProvider: '',      // which provider is authenticating
oauthRevokingProvider: '',    // which provider is being revoked
:disabled="oauthActiveProvider === provider.provider_name"
:disabled="oauthRevokingProvider === provider.provider_name"
```

For multiple concurrent operations, replace the entire object:
```js
this.oauthUsageLoading = Object.assign({}, this.oauthUsageLoading, { [name]: true });
```

### Alpine.js `x-show` vs `x-if` inside `x-for`

Use `x-show` directly on buttons. Do **not** combine `x-show` with `style="display:inline-flex"` — Alpine sets `display:none` which conflicts.

```html
<!-- Correct -->
<button x-show="condition" @click="handler()" type="button">...</button>
```

### Popup race condition fix

`_oauthWaitForCallback` uses a `resolved` flag with 500ms grace period before rejecting on popup close:

```js
const checkClosed = setInterval(() => {
    if (resolved) return;
    if (popup.closed) {
        setTimeout(() => {
            if (!resolved) { cleanup(); reject(new Error('Authentication cancelled')); }
        }, 500);
        clearInterval(checkClosed);
    }
}, 500);
```

### Post-revoke request blocking

When token is missing after revoke, cancel the request context so the upstream call is aborted:

```go
func (p *Provider) setOAuthHeader(req *http.Request) {
    token, err := p.oauth.getValidAccessToken(req.Context())
    if err != nil {
        slog.Error("oauth: cannot obtain access token", ...)
        ctx, cancel := context.WithCancelCause(req.Context())
        cancel(err)
        *req = *req.WithContext(ctx)
        return
    }
    req.Header.Set("Authorization", "Bearer "+token)
}
```

---

## Checklist for adding a new OAuth provider (e.g. Codex)

- [ ] Create `internal/oauth/{provider}.go` implementing `oauth.Provider`
- [ ] Add provider client ID, scopes, and endpoint constants
- [ ] Verify authorization URL parameter order requirements
- [ ] Verify state format requirements (hex vs base64url)
- [ ] Verify redirect URI requirements (which URIs the client ID accepts)
- [ ] Determine the manual/remote callback URI if different from local
- [ ] Register provider type in `OAuthHandler` provider dispatch
- [ ] Add tests in `internal/oauth/{provider}_test.go`
- [ ] Update dashboard sidebar link if needed

---

## Commits

| Commit | Description |
|---|---|
| `4566ee6` | `feat(anthropic): add OAuth 2.0 with PKCE authentication support` — initial implementation |
| `4fe9e5f` | `fix(oauth): fix dashboard OAuth page rendering and button interactivity` — all dashboard fixes |
| (pending) | `fix(oauth): support remote servers via manual callback flow` — remote support, revoke fix |
