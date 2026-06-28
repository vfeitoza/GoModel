package intelligentrouter

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoutingMemoryStore_AddGetAndLimit(t *testing.T) {
	store := &routingMemoryStore{data: make(map[string][]routingMemoryEntry)}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < routingMemoryMaxEntries+7; i++ {
		store.add("/team", "conv-1", fmt.Sprintf("model-%d", i), now.Add(time.Duration(i)*time.Minute))
	}
	// Read while all entries are still within the retention window so the test
	// exercises the cap-to-50 behavior, not expiry.
	history := store.get("/team", "conv-1", 5, now.Add(30*time.Minute))
	require.Len(t, history, 5)
	require.Equal(t, []string{"model-52", "model-53", "model-54", "model-55", "model-56"}, history)
}

func TestRoutingMemoryStore_ExpiresOldEntries(t *testing.T) {
	store := &routingMemoryStore{data: make(map[string][]routingMemoryEntry)}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store.add("/team", "conv-1", "old", now)
	store.add("/team", "conv-1", "fresh", now.Add(30*time.Minute))
	history := store.get("/team", "conv-1", 10, now.Add(routingMemoryMaxAge+30*time.Minute))
	require.Equal(t, []string{"fresh"}, history)
}

func TestRoutingMemoryStore_EmptyConversationIDReturnsNothing(t *testing.T) {
	store := &routingMemoryStore{data: make(map[string][]routingMemoryEntry)}
	now := time.Now()
	store.add("/team", "", "model-a", now)
	history := store.get("/team", "", 5, now)
	require.Nil(t, history)
}
