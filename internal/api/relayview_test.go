package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

func TestRankedKeys_OrdersByCountThenName(t *testing.T) {
	got := rankedKeys(map[string]int{"art": 1, "bitcoin": 12, "nostr": 8})
	want := []string{"bitcoin", "nostr", "art"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rankedKeys = %v, want %v", got, want)
	}
}

// Go randomises map iteration order. Without the name tiebreak the output
// reshuffles between identical requests, which makes chips jump around, defeats
// HTTP caching and produces flaky tests. Equal counts are the common case, so
// this is the path that matters.
func TestRankedKeys_TiesAreDeterministic(t *testing.T) {
	counts := map[string]int{"zebra": 5, "apple": 5, "mango": 5}
	first := rankedKeys(counts)
	for i := 0; i < 50; i++ {
		if got := rankedKeys(counts); !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d returned %v, first returned %v — order is not stable", i, got, first)
		}
	}
	if want := []string{"apple", "mango", "zebra"}; !reflect.DeepEqual(first, want) {
		t.Errorf("tie order = %v, want %v (name ascending)", first, want)
	}
}

func TestRankedKeys_EmptyIsNilNotEmptySlice(t *testing.T) {
	// nil so `omitempty` drops the field entirely rather than emitting `[]`.
	// An empty array reads to a client as "we checked and there are none",
	// which is a different claim from "unknown".
	if got := rankedKeys(nil); got != nil {
		t.Errorf("rankedKeys(nil) = %v, want nil", got)
	}
	if got := rankedKeys(map[string]int{}); got != nil {
		t.Errorf("rankedKeys(empty) = %v, want nil", got)
	}
}

func TestTopKey_HighestCountWithDeterministicTie(t *testing.T) {
	if got := topKey(map[string]int{"friendly": 12, "technical": 3}); got != "friendly" {
		t.Errorf("topKey = %q, want %q", got, "friendly")
	}
	if got := topKey(map[string]int{"beta": 7, "alpha": 7}); got != "alpha" {
		t.Errorf("tie topKey = %q, want %q (name ascending)", got, "alpha")
	}
	if got := topKey(nil); got != "" {
		t.Errorf("topKey(nil) = %q, want empty", got)
	}
}

// The whole design rests on encoding/json giving the shallowest field the name:
// RelayView.Topics (depth 0) must win over the embedded cache.RelayEntry.Topics
// (depth 1). If that ever stopped holding, `topics` would silently revert to a
// map and the UI's `.map()` would throw on real data.
func TestRelayView_EmitsBothShapes(t *testing.T) {
	view := NewRelayView(cache.RelayEntry{
		URL:        "wss://relay.example",
		Topics:     map[string]int{"art": 1, "bitcoin": 12, "nostr": 8},
		Atmosphere: map[string]int{"friendly": 9, "technical": 2},
	})

	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// topics must be the ARRAY (the UI's shape), not the embedded map.
	topics, ok := out["topics"].([]any)
	if !ok {
		t.Fatalf("topics is %T, want []any — field shadowing is not applying", out["topics"])
	}
	if len(topics) != 3 || topics[0] != "bitcoin" {
		t.Errorf("topics = %v, want ranked list starting with bitcoin", topics)
	}

	// counts must survive alongside it.
	counts, ok := out["topic_counts"].(map[string]any)
	if !ok {
		t.Fatalf("topic_counts is %T, want object", out["topic_counts"])
	}
	if counts["bitcoin"] != float64(12) {
		t.Errorf("topic_counts[bitcoin] = %v, want 12", counts["bitcoin"])
	}

	// atmosphere is the scalar the UI renders; its distribution is kept too.
	if out["atmosphere"] != "friendly" {
		t.Errorf("atmosphere = %v, want \"friendly\"", out["atmosphere"])
	}
	if _, ok := out["atmosphere_counts"].(map[string]any); !ok {
		t.Fatalf("atmosphere_counts is %T, want object", out["atmosphere_counts"])
	}
}

// With no annotations both shapes must vanish, not appear as empty containers.
func TestRelayView_OmitsEmptyAnnotations(t *testing.T) {
	raw, err := json.Marshal(NewRelayView(cache.RelayEntry{URL: "wss://relay.example"}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"topics", "topic_counts", "atmosphere", "atmosphere_counts"} {
		if _, present := out[k]; present {
			t.Errorf("%q present on an unannotated relay; want omitted", k)
		}
	}
}

// The embedded entry's other fields must still serialize — the view adds to the
// payload, it does not replace it.
func TestRelayView_PreservesEmbeddedFields(t *testing.T) {
	raw, err := json.Marshal(NewRelayView(cache.RelayEntry{
		URL:           "wss://relay.example",
		Name:          "Example",
		SupportedNIPs: []int{1, 11},
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["url"] != "wss://relay.example" || out["name"] != "Example" {
		t.Errorf("embedded fields lost: url=%v name=%v", out["url"], out["name"])
	}
	if nips, ok := out["supported_nips"].([]any); !ok || len(nips) != 2 {
		t.Errorf("supported_nips = %v, want 2 entries", out["supported_nips"])
	}
}

func TestNewRelayViews_NilStaysNil(t *testing.T) {
	// `relays: null` vs `relays: []` is a real distinction for clients; keep
	// whichever the handler produced rather than inventing one.
	if got := NewRelayViews(nil); got != nil {
		t.Errorf("NewRelayViews(nil) = %v, want nil", got)
	}
}
