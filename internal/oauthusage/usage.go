// Package oauthusage fetches and caches OAuth usage data from the Anthropic API.
// Usage data includes rate-limit windows (5-hour, 7-day) and extra credit usage.
package oauthusage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	anthropicUsageURL  = "https://api.anthropic.com/api/oauth/usage"
	cacheStaleAfter    = 5 * time.Minute
	unsupportedRetryIn = 30 * time.Minute
)

// UsageWindow describes utilization within a rolling time window.
type UsageWindow struct {
	// Utilization is a value between 0 and 1 (e.g. 0.45 = 45%).
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// UtilizationPercent returns the utilization as an integer percentage (0–100).
func (w *UsageWindow) UtilizationPercent() int {
	if w == nil {
		return 0
	}
	pct := w.Utilization * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return int(pct)
}

// ExtraUsage describes pay-as-you-go credit usage beyond the subscription.
type ExtraUsage struct {
	IsEnabled      bool    `json:"is_enabled"`
	MonthlyLimit   float64 `json:"monthly_limit,omitempty"`
	UsedCredits    float64 `json:"used_credits,omitempty"`
	Utilization    float64 `json:"utilization,omitempty"`
	DisabledReason string  `json:"disabled_reason,omitempty"`
}

// Usage holds the full usage snapshot for an OAuth-authenticated account.
type Usage struct {
	FiveHour          *UsageWindow `json:"five_hour,omitempty"`
	SevenDay          *UsageWindow `json:"seven_day,omitempty"`
	SevenDayOAuthApps *UsageWindow `json:"seven_day_oauth_apps,omitempty"`
	SevenDayOpus      *UsageWindow `json:"seven_day_opus,omitempty"`
	SevenDaySonnet    *UsageWindow `json:"seven_day_sonnet,omitempty"`
	ExtraUsage        *ExtraUsage  `json:"extra_usage,omitempty"`
	FetchedAt         time.Time    `json:"fetched_at"`
}

// Fetcher retrieves OAuth usage data for an access token.
type Fetcher interface {
	FetchUsage(ctx context.Context, accessToken string) (*Usage, error)
}

// cacheEntry holds a cached usage result for one provider.
type cacheEntry struct {
	usage       *Usage
	fetchedAt   time.Time
	unsupported bool // true when the API returned "not supported" for this token
}

func (e *cacheEntry) isStale() bool {
	if e == nil || e.fetchedAt.IsZero() {
		return true
	}
	return time.Since(e.fetchedAt) > cacheStaleAfter
}

func (e *cacheEntry) unsupportedStillFresh() bool {
	if e == nil || !e.unsupported {
		return false
	}
	return time.Since(e.fetchedAt) < unsupportedRetryIn
}

// CachingFetcher wraps an HTTP fetcher with an in-memory cache keyed by
// provider name. Cache entries expire after 5 minutes.
type CachingFetcher struct {
	mu      sync.Mutex
	cache   map[string]*cacheEntry
	fetcher Fetcher
}

// NewCachingFetcher creates a CachingFetcher backed by the given Fetcher.
func NewCachingFetcher(fetcher Fetcher) *CachingFetcher {
	return &CachingFetcher{
		cache:   make(map[string]*cacheEntry),
		fetcher: fetcher,
	}
}

// FetchUsage returns cached usage if fresh, otherwise fetches from the API.
func (c *CachingFetcher) FetchUsage(ctx context.Context, providerName, accessToken string) (*Usage, error) {
	c.mu.Lock()
	entry := c.cache[providerName]
	if entry != nil && !entry.isStale() {
		usage := entry.usage
		c.mu.Unlock()
		return usage, nil
	}
	if entry != nil && entry.unsupportedStillFresh() {
		c.mu.Unlock()
		return nil, nil // unsupported, skip silently
	}
	c.mu.Unlock()

	usage, err := c.fetcher.FetchUsage(ctx, accessToken)
	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		// Mark as unsupported if the API explicitly says so
		if isUnsupportedError(err) {
			c.cache[providerName] = &cacheEntry{
				fetchedAt:   time.Now(),
				unsupported: true,
			}
			return nil, nil
		}
		return nil, err
	}

	c.cache[providerName] = &cacheEntry{
		usage:     usage,
		fetchedAt: time.Now(),
	}
	return usage, nil
}

// Invalidate removes the cached entry for the given provider.
func (c *CachingFetcher) Invalidate(providerName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, providerName)
}

// HTTPFetcher fetches usage data from the Anthropic OAuth usage API.
type HTTPFetcher struct {
	client   *http.Client
	usageURL string // overridable for tests
}

// NewHTTPFetcher creates an HTTPFetcher using the default HTTP client.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{
		client:   &http.Client{Timeout: 15 * time.Second},
		usageURL: anthropicUsageURL,
	}
}

// NewHTTPFetcherWithURL creates an HTTPFetcher with a custom usage URL (for tests).
func NewHTTPFetcherWithURL(usageURL string) *HTTPFetcher {
	return &HTTPFetcher{
		client:   &http.Client{Timeout: 15 * time.Second},
		usageURL: usageURL,
	}
}

// FetchUsage calls the Anthropic OAuth usage API and returns normalized data.
func (f *HTTPFetcher) FetchUsage(ctx context.Context, accessToken string) (*Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.usageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch oauth usage: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusUnauthorized {
		// Check for explicit "not supported" message
		if isUnsupportedBody(body) {
			return nil, &UnsupportedError{Message: string(body)}
		}
		return nil, fmt.Errorf("oauth usage: unauthorized (%d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth usage: unexpected status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	return parseUsageResponse(body)
}

// UnsupportedError indicates the usage API does not support this OAuth token.
type UnsupportedError struct {
	Message string
}

func (e *UnsupportedError) Error() string {
	return "oauth usage API not supported for this token: " + e.Message
}

func isUnsupportedError(err error) bool {
	_, ok := err.(*UnsupportedError)
	return ok
}

func isUnsupportedBody(body []byte) bool {
	lower := string(body)
	return len(lower) > 0 && (contains(lower, "not supported") || contains(lower, "oauth authentication is currently not supported"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// rawUsagePayload mirrors the Anthropic OAuth usage API response shape.
type rawUsagePayload struct {
	FiveHour          *rawWindow    `json:"five_hour"`
	SevenDay          *rawWindow    `json:"seven_day"`
	SevenDayOAuthApps *rawWindow    `json:"seven_day_oauth_apps"`
	SevenDayOpus      *rawWindow    `json:"seven_day_opus"`
	SevenDaySonnet    *rawWindow    `json:"seven_day_sonnet"`
	ExtraUsage        *rawExtraUsage `json:"extra_usage"`
}

type rawWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

type rawExtraUsage struct {
	MonthlyLimit   *float64 `json:"monthly_limit"`
	UsedCredits    *float64 `json:"used_credits"`
	Utilization    *float64 `json:"utilization"`
	DisabledReason string   `json:"disabled_reason"`
}

func parseUsageResponse(body []byte) (*Usage, error) {
	var raw rawUsagePayload
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse oauth usage response: %w", err)
	}

	usage := &Usage{
		FetchedAt:         time.Now().UTC(),
		FiveHour:          normalizeWindow(raw.FiveHour),
		SevenDay:          normalizeWindow(raw.SevenDay),
		SevenDayOAuthApps: normalizeWindow(raw.SevenDayOAuthApps),
		SevenDayOpus:      normalizeWindow(raw.SevenDayOpus),
		SevenDaySonnet:    normalizeWindow(raw.SevenDaySonnet),
		ExtraUsage:        normalizeExtraUsage(raw.ExtraUsage),
	}
	return usage, nil
}

func normalizeWindow(raw *rawWindow) *UsageWindow {
	if raw == nil {
		return nil
	}
	w := &UsageWindow{
		Utilization: clampUtilization(raw.Utilization),
	}
	if raw.ResetsAt != "" {
		if t, err := time.Parse(time.RFC3339, raw.ResetsAt); err == nil {
			w.ResetsAt = t.UTC()
		}
	}
	return w
}

func normalizeExtraUsage(raw *rawExtraUsage) *ExtraUsage {
	if raw == nil {
		return nil
	}
	e := &ExtraUsage{
		IsEnabled:      raw.DisabledReason == "",
		DisabledReason: raw.DisabledReason,
	}
	if raw.MonthlyLimit != nil {
		e.MonthlyLimit = *raw.MonthlyLimit
	}
	if raw.UsedCredits != nil {
		e.UsedCredits = *raw.UsedCredits
	}
	if raw.Utilization != nil {
		e.Utilization = clampUtilization(*raw.Utilization)
	}
	return e
}

func clampUtilization(v float64) float64 {
	if v < 0 {
		return 0
	}
	// The Anthropic API may return utilization as a fraction (0–1) or as a
	// percentage (0–100). Values above 1 are treated as already-percentage.
	// Normalize to 0–1 range to match UsageWindow.Utilization semantics.
	if v > 1 {
		v = v / 100
	}
	if v > 1 {
		return 1
	}
	return v
}
