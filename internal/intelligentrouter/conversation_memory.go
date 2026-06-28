package intelligentrouter

import (
	"strings"
	"sync"
	"time"
)

const (
	routingMemoryMaxAge    = time.Hour
	routingMemoryMaxEntries = 50
	routingMemoryDefaultN   = 5
)

type routingMemoryEntry struct {
	model string
	ts    time.Time
}

type routingMemoryStore struct {
	mu   sync.Mutex
	data map[string][]routingMemoryEntry
}

var defaultRoutingMemory = &routingMemoryStore{data: make(map[string][]routingMemoryEntry)}

func routingMemoryKey(userPath, conversationID string) string {
	userPath = strings.TrimSpace(userPath)
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ""
	}
	return userPath + "::" + conversationID
}

func addRoutingDecision(userPath, conversationID, model string) {
	defaultRoutingMemory.add(userPath, conversationID, model, time.Now())
}

func getRoutingHistory(userPath, conversationID string, count int) []string {
	return defaultRoutingMemory.get(userPath, conversationID, count, time.Now())
}

func (s *routingMemoryStore) add(userPath, conversationID, model string, now time.Time) {
	if s == nil {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	key := routingMemoryKey(userPath, conversationID)
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	entries := append(s.data[key], routingMemoryEntry{model: model, ts: now})
	if len(entries) > routingMemoryMaxEntries {
		entries = entries[len(entries)-routingMemoryMaxEntries:]
	}
	s.data[key] = entries
}

func (s *routingMemoryStore) get(userPath, conversationID string, count int, now time.Time) []string {
	if s == nil {
		return nil
	}
	key := routingMemoryKey(userPath, conversationID)
	if key == "" {
		return nil
	}
	if count <= 0 {
		count = routingMemoryDefaultN
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	entries := s.data[key]
	if len(entries) == 0 {
		return nil
	}
	if count > len(entries) {
		count = len(entries)
	}
	entries = entries[len(entries)-count:]
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.model)
	}
	return out
}

func (s *routingMemoryStore) cleanupLocked(now time.Time) {
	cutoff := now.Add(-routingMemoryMaxAge)
	for key, entries := range s.data {
		kept := entries[:0]
		for _, e := range entries {
			if e.ts.Before(cutoff) {
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(s.data, key)
			continue
		}
		s.data[key] = append([]routingMemoryEntry(nil), kept...)
	}
}
