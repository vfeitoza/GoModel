package routingstate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gomodel/internal/core"
	"gomodel/internal/routing"
)

type snapshot struct {
	order           []string
	byKey           map[string]Entry
	providers       map[string]Entry
	canonicalModels map[string]Entry
	candidates      map[string]Entry
}

type Service struct {
	store     Store
	mu        sync.RWMutex
	snapshot  snapshot
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	return &Service{store: store, snapshot: snapshot{
		order:           []string{},
		byKey:           map[string]Entry{},
		providers:       map[string]Entry{},
		canonicalModels: map[string]Entry{},
		candidates:      map[string]Entry{},
	}}, nil
}

func (s *Service) Refresh(ctx context.Context) error {
	entries, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("list routing state: %w", err)
	}
	next := snapshot{
		order:           make([]string, 0, len(entries)),
		byKey:           make(map[string]Entry, len(entries)),
		providers:       make(map[string]Entry),
		canonicalModels: make(map[string]Entry),
		candidates:      make(map[string]Entry),
	}
	for _, entry := range entries {
		normalized, err := normalizeEntry(entry)
		if err != nil {
			return fmt.Errorf("load routing state %q: %w", entry.Key, err)
		}
		next.order = append(next.order, normalized.Key)
		next.byKey[normalized.Key] = normalized
		switch normalized.Kind {
		case KindProvider:
			next.providers[normalized.ProviderName] = normalized
		case KindCanonicalModel:
			next.canonicalModels[normalized.CanonicalModel] = normalized
		case KindPoolCandidate:
			next.candidates[normalized.ProviderName+"/"+normalized.Model] = normalized
		}
	}
	sort.Strings(next.order)

	s.mu.Lock()
	s.snapshot = next
	s.mu.Unlock()
	return nil
}

func (s *Service) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Entry, 0, len(s.snapshot.order))
	for _, key := range s.snapshot.order {
		result = append(result, s.snapshot.byKey[key])
	}
	return result
}

func (s *Service) Upsert(ctx context.Context, entry Entry) error {
	normalized, err := normalizeEntry(entry)
	if err != nil {
		return err
	}
	if err := s.store.Upsert(ctx, normalized); err != nil {
		return err
	}
	return s.Refresh(ctx)
}

func (s *Service) Delete(ctx context.Context, key string) error {
	if err := s.store.Delete(ctx, strings.TrimSpace(key)); err != nil {
		return err
	}
	return s.Refresh(ctx)
}

func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *Service) StartBackgroundRefresh(interval time.Duration) func() {
	if interval <= 0 {
		interval = time.Minute
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = s.Refresh(context.Background())
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

func (s *Service) ProviderEnabled(name string) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.snapshot.providers[strings.TrimSpace(name)]
	if !ok {
		return true
	}
	return entry.Enabled
}

func (s *Service) CanonicalModelEnabled(name string) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.snapshot.canonicalModels[strings.TrimSpace(name)]
	if !ok {
		return true
	}
	return entry.Enabled
}

func (s *Service) CandidateEnabled(selector core.ModelSelector) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.snapshot.candidates[strings.TrimSpace(selector.Provider)+"/"+strings.TrimSpace(selector.Model)]
	if !ok {
		return true
	}
	return entry.Enabled
}

func (s *Service) FilterCandidates(canonical string, candidates []routing.Candidate) []routing.Candidate {
	if s == nil {
		return append([]routing.Candidate(nil), candidates...)
	}
	if !s.CanonicalModelEnabled(canonical) {
		return nil
	}
	filtered := make([]routing.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		selector := core.ModelSelector{Provider: candidate.Provider, Model: candidate.Model}
		if !s.ProviderEnabled(candidate.Provider) {
			continue
		}
		if !s.CandidateEnabled(selector) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}
