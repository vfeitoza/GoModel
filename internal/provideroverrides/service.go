package provideroverrides

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type snapshot struct {
	byProviderName map[string]ProviderOverride
	defaultEnabled bool
}

// Service manages provider override state in memory with periodic refresh from storage.
type Service struct {
	store          Store
	catalog        Catalog
	defaultEnabled bool
	current        atomic.Value // holds snapshot
	refreshMu      sync.Mutex
}

// NewService creates a new provider override service.
func NewService(store Store, catalog Catalog, defaultEnabled bool) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}

	service := &Service{
		store:          store,
		catalog:        catalog,
		defaultEnabled: defaultEnabled,
	}
	service.current.Store(snapshot{
		byProviderName: make(map[string]ProviderOverride),
		defaultEnabled: defaultEnabled,
	})
	return service, nil
}

// DefaultEnabled reports the default enabled state for providers without overrides.
func (s *Service) DefaultEnabled() bool {
	if s == nil {
		return DefaultEnabledProviders
	}
	return s.defaultEnabled
}

// Refresh reloads overrides from storage and atomically swaps the snapshot.
func (s *Service) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshLocked(ctx)
}

func (s *Service) refreshLocked(ctx context.Context) error {
	overrides, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("list provider overrides: %w", err)
	}
	next, err := s.buildSnapshot(overrides)
	if err != nil {
		return err
	}
	s.current.Store(next)
	return nil
}

func (s *Service) snapshot() snapshot {
	if s == nil {
		return snapshot{
			byProviderName: make(map[string]ProviderOverride),
			defaultEnabled: DefaultEnabledProviders,
		}
	}
	return s.current.Load().(snapshot)
}

func (s *Service) buildSnapshot(overrides []ProviderOverride) (snapshot, error) {
	next := snapshot{
		byProviderName: make(map[string]ProviderOverride, len(overrides)),
		defaultEnabled: s.defaultEnabled,
	}

	for _, override := range overrides {
		normalized := normalizeStoredOverride(override)
		next.byProviderName[normalized.ProviderName] = normalized
	}

	return next, nil
}

// List returns all cached overrides sorted by provider name.
func (s *Service) List() []ProviderOverride {
	snap := s.snapshot()
	result := make([]ProviderOverride, 0, len(snap.byProviderName))
	for _, override := range snap.byProviderName {
		result = append(result, override.Clone())
	}
	sortOverrides(result)
	return result
}

// ListViews returns all cached overrides with view wrapper.
func (s *Service) ListViews() []View {
	overrides := s.List()
	result := make([]View, 0, len(overrides))
	for _, override := range overrides {
		result = append(result, NewView(override))
	}
	return result
}

// Get returns one cached override by provider name.
func (s *Service) Get(providerName string) (*ProviderOverride, bool) {
	providerName = normalizeProviderName(providerName)
	if providerName == "" {
		return nil, false
	}

	snap := s.snapshot()
	override, ok := snap.byProviderName[providerName]
	if !ok {
		return nil, false
	}
	result := override.Clone()
	return &result, true
}

// Enabled reports whether a provider is enabled.
// Returns the override value if set, or the default enabled state otherwise.
func (s *Service) Enabled(providerName string) bool {
	providerName = normalizeProviderName(providerName)
	if providerName == "" {
		return DefaultEnabledProviders
	}

	snap := s.snapshot()
	if override, ok := snap.byProviderName[providerName]; ok {
		return override.Enabled
	}
	return snap.defaultEnabled
}

// Upsert validates and stores one override, then refreshes the in-memory snapshot.
func (s *Service) Upsert(ctx context.Context, override ProviderOverride) error {
	if s == nil {
		return fmt.Errorf("provider override service is required")
	}

	normalized, err := normalizeProviderOverride(s.catalog, override)
	if err != nil {
		return err
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	current := s.snapshot()
	if _, err := s.buildSnapshot(appendOverridesFromSnapshot(current, normalized)); err != nil {
		return fmt.Errorf("validate provider overrides: %w", err)
	}

	previous, existed := current.byProviderName[normalized.ProviderName]
	if err := s.store.Upsert(ctx, normalized); err != nil {
		return fmt.Errorf("upsert provider override: %w", err)
	}
	if err := s.refreshLocked(ctx); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var rollbackErr error
		if existed {
			rollbackErr = s.store.Upsert(rollbackCtx, previous)
		} else {
			rollbackErr = s.store.Delete(rollbackCtx, normalized.ProviderName)
		}
		if rollbackErr != nil {
			return fmt.Errorf("refresh provider overrides: %w (rollback failed: %v)", err, rollbackErr)
		}
		return fmt.Errorf("refresh provider overrides: %w", err)
	}
	return nil
}

// Delete removes one override and refreshes the in-memory snapshot.
func (s *Service) Delete(ctx context.Context, providerName string) error {
	if s == nil {
		return fmt.Errorf("provider override service is required")
	}

	providerName = normalizeProviderName(providerName)
	if providerName == "" {
		return fmt.Errorf("provider_name is required")
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	current := s.snapshot()
	if _, err := s.buildSnapshot(deleteOverrideFromSnapshot(current, providerName)); err != nil {
		return fmt.Errorf("validate provider overrides: %w", err)
	}

	previous, existed := current.byProviderName[providerName]
	if err := s.store.Delete(ctx, providerName); err != nil {
		return fmt.Errorf("delete provider override: %w", err)
	}
	if err := s.refreshLocked(ctx); err != nil {
		if !existed {
			return fmt.Errorf("refresh provider overrides: %w", err)
		}

		rollbackCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if rollbackErr := s.store.Upsert(rollbackCtx, previous); rollbackErr != nil {
			return fmt.Errorf("refresh provider overrides: %w (rollback failed: %v)", err, rollbackErr)
		}
		return fmt.Errorf("refresh provider overrides: %w", err)
	}
	return nil
}

// StartBackgroundRefresh periodically reloads provider overrides from storage until stopped.
func (s *Service) StartBackgroundRefresh(interval time.Duration) func() {
	if interval <= 0 {
		interval = time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshCtx, refreshCancel := context.WithTimeout(ctx, 30*time.Second)
				if err := s.Refresh(refreshCtx); err != nil {
					slog.Error("failed to refresh provider overrides", "error", err)
				}
				refreshCancel()
			}
		}
	}()

	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

// appendOverridesFromSnapshot creates a new override list from snapshot plus a new override.
func appendOverridesFromSnapshot(snap snapshot, override ProviderOverride) []ProviderOverride {
	result := make([]ProviderOverride, 0, len(snap.byProviderName)+1)
	for _, o := range snap.byProviderName {
		result = append(result, o.Clone())
	}
	result = append(result, override.Clone())
	return result
}

// deleteOverrideFromSnapshot creates a new override list from snapshot without the specified provider.
func deleteOverrideFromSnapshot(snap snapshot, providerName string) []ProviderOverride {
	result := make([]ProviderOverride, 0, len(snap.byProviderName))
	for name, o := range snap.byProviderName {
		if name == providerName {
			continue
		}
		result = append(result, o.Clone())
	}
	return result
}

// normalizeProviderOverride normalizes and validates a provider override.
func normalizeProviderOverride(catalog Catalog, override ProviderOverride) (ProviderOverride, error) {
	normalized := ProviderOverride{
		ProviderName: normalizeProviderName(override.ProviderName),
		Enabled:      override.Enabled,
		CreatedAt:    override.CreatedAt,
		UpdatedAt:    override.UpdatedAt,
	}

	if normalized.ProviderName == "" {
		return normalized, fmt.Errorf("provider_name is required")
	}

	if catalog != nil && !catalog.ProviderExists(normalized.ProviderName) {
		return normalized, fmt.Errorf("provider does not exist: %s", normalized.ProviderName)
	}

	return normalized, nil
}

// ProviderEnabledChecker is the interface used by fallback resolver and router.
type ProviderEnabledChecker interface {
	Enabled(providerName string) bool
}

// Ensure Service implements ProviderEnabledChecker.
var _ ProviderEnabledChecker = (*Service)(nil)