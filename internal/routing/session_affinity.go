package routing

import (
	"context"
	"strings"
	"sync"
	"time"

	"gomodel/internal/core"
)

type affinityStore struct {
	mu      sync.Mutex
	entries map[string]affinityEntry
	now     func() time.Time
	ttl     time.Duration
	enabled bool
}

type affinityEntry struct {
	selector  Candidate
	expiresAt time.Time
}

func newAffinityStore(enabled bool, ttl time.Duration) *affinityStore {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &affinityStore{
		entries: make(map[string]affinityEntry),
		now:     time.Now,
		ttl:     ttl,
		enabled: enabled,
	}
}

func affinityKey(ctx context.Context, canonicalModel string) string {
	canonicalModel = strings.TrimSpace(canonicalModel)
	if canonicalModel == "" {
		return ""
	}
	userPath := strings.TrimSpace(core.UserPathFromContext(ctx))
	if userPath != "" {
		return canonicalModel + "|user_path|" + userPath
	}
	requestID := strings.TrimSpace(core.GetRequestID(ctx))
	if requestID != "" {
		return canonicalModel + "|request_id|" + requestID
	}
	return ""
}

func (s *affinityStore) Get(ctx context.Context, canonicalModel string) (Candidate, bool) {
	if s == nil || !s.enabled {
		return Candidate{}, false
	}
	key := affinityKey(ctx, canonicalModel)
	if key == "" {
		return Candidate{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return Candidate{}, false
	}
	if !entry.expiresAt.After(s.now()) {
		delete(s.entries, key)
		return Candidate{}, false
	}
	return entry.selector, true
}

func (s *affinityStore) Put(ctx context.Context, canonicalModel string, candidate Candidate) {
	if s == nil || !s.enabled {
		return
	}
	key := affinityKey(ctx, canonicalModel)
	if key == "" {
		return
	}
	s.mu.Lock()
	s.entries[key] = affinityEntry{selector: candidate, expiresAt: s.now().Add(s.ttl)}
	s.mu.Unlock()
}
