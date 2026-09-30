package publisher

import (
	"testing"
	"time"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

func TestHashRelayData_Deterministic(t *testing.T) {
	entry := &cache.RelayEntry{
		URL:           "wss://relay.example.com",
		Name:          "Example",
		Health:        "online",
		LatencyMs:     120,
		SupportedNIPs: []int{1, 11, 42},
		Software:      "strfry",
		Version:       "1.0.0",
	}

	h1 := hashRelayData(entry)
	h2 := hashRelayData(entry)
	if h1 != h2 {
		t.Errorf("same entry produced different hashes: %s vs %s", h1, h2)
	}
}

func TestHashRelayData_IgnoresLastChecked(t *testing.T) {
	entry := &cache.RelayEntry{
		URL:         "wss://relay.example.com",
		Health:      "online",
		LastChecked: time.Now(),
	}
	h1 := hashRelayData(entry)

	entry.LastChecked = time.Now().Add(5 * time.Minute)
	h2 := hashRelayData(entry)

	if h1 != h2 {
		t.Error("hash changed when only LastChecked changed")
	}
}

func TestHashRelayData_QuantizesLatency(t *testing.T) {
	entry := &cache.RelayEntry{
		URL:       "wss://relay.example.com",
		Health:    "online",
		LatencyMs: 120,
	}
	h1 := hashRelayData(entry)

	entry.LatencyMs = 130
	h2 := hashRelayData(entry)
	if h1 != h2 {
		t.Error("hash changed for latency within same 50ms bucket (120 vs 130)")
	}

	entry.LatencyMs = 160
	h3 := hashRelayData(entry)
	if h1 == h3 {
		t.Error("hash should change when latency crosses a 50ms bucket (120 vs 160)")
	}
}

func TestHashRelayData_DetectsHealthChange(t *testing.T) {
	entry := &cache.RelayEntry{
		URL:    "wss://relay.example.com",
		Health: "online",
	}
	h1 := hashRelayData(entry)

	entry.Health = "offline"
	h2 := hashRelayData(entry)

	if h1 == h2 {
		t.Error("hash should change when health changes")
	}
}

func TestHashRelayData_MapOrderIndependent(t *testing.T) {
	e1 := &cache.RelayEntry{
		URL:    "wss://relay.example.com",
		Health: "online",
		Topics: map[string]int{"bitcoin": 5, "nostr": 3, "privacy": 1},
	}
	e2 := &cache.RelayEntry{
		URL:    "wss://relay.example.com",
		Health: "online",
		Topics: map[string]int{"privacy": 1, "bitcoin": 5, "nostr": 3},
	}

	h1 := hashRelayData(e1)
	h2 := hashRelayData(e2)
	if h1 != h2 {
		t.Error("hash should be independent of map iteration order")
	}
}

func TestHashRelayData_NIPOrderIndependent(t *testing.T) {
	e1 := &cache.RelayEntry{
		URL:           "wss://relay.example.com",
		Health:        "online",
		SupportedNIPs: []int{42, 1, 11},
	}
	e2 := &cache.RelayEntry{
		URL:           "wss://relay.example.com",
		Health:        "online",
		SupportedNIPs: []int{1, 11, 42},
	}

	if hashRelayData(e1) != hashRelayData(e2) {
		t.Error("hash should be independent of NIP slice order")
	}
}

func TestDeltaTracker_FirstCallAlwaysChanged(t *testing.T) {
	dt := newDeltaTracker()
	entry := &cache.RelayEntry{URL: "wss://relay.example.com", Health: "online"}

	if !dt.changed("wss://relay.example.com", entry) {
		t.Error("first call should always report changed")
	}
}

func TestDeltaTracker_UnchangedAfterRecord(t *testing.T) {
	dt := newDeltaTracker()
	entry := &cache.RelayEntry{URL: "wss://relay.example.com", Health: "online"}

	dt.record("wss://relay.example.com", entry)

	if dt.changed("wss://relay.example.com", entry) {
		t.Error("should report unchanged after recording same entry")
	}
}

func TestDeltaTracker_ChangedAfterUpdate(t *testing.T) {
	dt := newDeltaTracker()
	entry := &cache.RelayEntry{URL: "wss://relay.example.com", Health: "online"}

	dt.record("wss://relay.example.com", entry)

	entry.Health = "offline"
	if !dt.changed("wss://relay.example.com", entry) {
		t.Error("should report changed after health update")
	}
}

func TestDeltaTracker_NeedsFullRefresh(t *testing.T) {
	dt := newDeltaTracker()

	if !dt.needsFullRefresh(time.Hour) {
		t.Error("should need refresh when never published")
	}

	dt.markFullRefresh()
	if dt.needsFullRefresh(time.Hour) {
		t.Error("should not need refresh immediately after marking")
	}
}

func TestDeltaTracker_Prune(t *testing.T) {
	dt := newDeltaTracker()

	e1 := &cache.RelayEntry{URL: "wss://keep.example.com", Health: "online"}
	e2 := &cache.RelayEntry{URL: "wss://remove.example.com", Health: "online"}

	dt.record("wss://keep.example.com", e1)
	dt.record("wss://remove.example.com", e2)

	dt.prune(map[string]bool{"wss://keep.example.com": true})

	if dt.changed("wss://keep.example.com", e1) {
		t.Error("kept relay should still be tracked")
	}
	if !dt.changed("wss://remove.example.com", e2) {
		t.Error("pruned relay should report as changed (not tracked)")
	}
}
