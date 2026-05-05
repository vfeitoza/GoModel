# OAuth Authentication

GoModel supports OAuth 2.0 authentication for providers that offer it as an alternative to static API keys. Once authenticated, the provider works identically to one configured with a static key — no changes needed in your client applications.

Currently supported: **Anthropic (Claude)**

---

## Setup

### 1. Configure the provider

In your `config/config.yaml`, set `api_key` to the sentinel value `"oauth"`:

```yaml
providers:
  my_claude:
    type: anthropic
    api_key: "oauth"
```

You can name the provider anything you like. Multiple OAuth providers of the same type are supported:

```yaml
providers:
  claude_personal:
    type: anthropic
    api_key: "oauth"
  claude_work:
    type: anthropic
    api_key: "oauth"
```

### 2. Authenticate via the dashboard

Open the GoModel admin dashboard and navigate to **OAuth Providers**.

Each configured OAuth provider appears with its current status. Click **Authenticate** to start the flow.

### 3. Send requests

Once authenticated, send requests via the passthrough route:

```
POST /p/{provider_name}/v1/chat/completions
```

Example:

```bash
curl https://your-gomodel-instance/p/my_claude/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-gomodel-key" \
  -d '{
    "model": "claude-opus-4-5",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

---

## Authentication flow

### Local (same machine)

Click **Authenticate**. A popup opens, you authorize with your Anthropic account, and the popup closes automatically. No extra steps needed.

### Remote server

If GoModel is running on a remote server (e.g. `https://models.example.com`), the local callback cannot be reached from your browser. Use the **Remote** button instead:

1. Click **Remote** — a popup opens pointing to Anthropic's authorization page
2. Authorize with your Anthropic account
3. After authorizing, the browser lands on a page at `platform.claude.com` — copy the full URL from the address bar
4. Paste the URL into the field that appeared in the dashboard and click **Confirm**

GoModel extracts the authorization code from the URL and completes the token exchange automatically.

---

## Token management

### Automatic refresh

Access tokens are refreshed automatically before they expire (5-minute safety margin). No manual action needed.

### Disconnect

Click **Disconnect** on a provider card to revoke the stored token. Any in-flight requests using that provider will be rejected immediately.

### Re-authenticate

If a token expires and cannot be refreshed, the provider status changes to **Expired**. Click **Re-authenticate** to start a new OAuth flow.

---

## Usage monitoring

The dashboard shows rate-limit usage for each authenticated provider:

- **5-hour window** — short-term usage against Anthropic's rolling limit
- **7-day window** — weekly usage
- **Extra credits** — additional purchased credits (if applicable)

Click **Refresh** on the usage section to fetch the latest data.

---

## Security notes

- Tokens are stored in the same database as the rest of GoModel's data (SQLite by default, or PostgreSQL/MongoDB if configured)
- Tokens are never exposed in logs or API responses
- Revoking a token in the dashboard removes it from storage; it does not call Anthropic's token revocation endpoint
- The OAuth client ID used is the same public client ID used by Claude Code and other first-party Anthropic tools

---

## Troubleshooting

**"No OAuth providers configured"**
Make sure at least one provider has `api_key: "oauth"` in your config and restart GoModel.

**"Popup blocked"**
Allow popups for the GoModel admin dashboard in your browser settings.

**"Authentication cancelled"**
The popup was closed before the flow completed. Try again.

**"Invalid or expired OAuth state"**
The flow timed out (3-minute limit) or the page was refreshed. Click Authenticate again.

**Provider shows as Expired**
The access token expired and the refresh token is no longer valid. Click Re-authenticate.
