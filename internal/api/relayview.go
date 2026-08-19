package api

import (
	"sort"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

// RelayView is the API representation of a cache.RelayEntry.
//
// # WHY THIS EXISTS
//
// Topics and Atmosphere are stored as `map[string]int` — topic -> annotation
// count — because the count is the consensus signal this whole service is for.
// One person calling a relay "art" and twelve calling it "bitcoin" are very
// different claims, and flattening them to a bare list throws that away
// permanently for every consumer.
//
// The UI, however, is typed for `topics?: string[]` and renders
// `relay.topics?.length && relay.topics.map(...)`. Against a JSON object
// `.length` is undefined, so the chips silently never render — and if anything
// ever did get past that guard, `.map` on an object throws. Topics could
// therefore never display, no matter how much data existed.
//
// Rather than pick a side, the view emits BOTH:
//
//	topics            ["bitcoin","nostr","art"]        <- matches the UI today
//	topic_counts      {"bitcoin":12,"nostr":8,"art":1} <- keeps the signal
//
// The map remains the single source of truth; the list is DERIVED here at
// response-build time. Storing both would let them drift, and the list is the
// one users see.
//
// The cache struct is deliberately untouched. cache.RelayEntry is also
// marshalled into Redis, so renaming its json tags would make every entry
// written by an older build unreadable until its TTL expired — a self-inflicted
// outage in exchange for a field rename.
//
// Field shadowing: `Topics` and `Atmosphere` here sit at depth 0 while the
// embedded cache.RelayEntry's versions are at depth 1, and encoding/json gives
// the shallowest field the name. That is what lets `topics` change shape on the
// wire without touching the embedded struct.
type RelayView struct {
	cache.RelayEntry

	// Ranked list, highest annotation count first. Shadows the embedded map.
	Topics []string `json:"topics,omitempty"`
	// Full distribution, preserved for clients that want to weight or rank.
	TopicCounts map[string]int `json:"topic_counts,omitempty"`

	// Single highest-count atmosphere; the UI treats this as a scalar.
	// Shadows the embedded map.
	Atmosphere string `json:"atmosphere,omitempty"`
	// Full distribution. Worth keeping separately: a 12-vs-11 split and a
	// unanimous 12 both render as one word without it.
	AtmosphereCounts map[string]int `json:"atmosphere_counts,omitempty"`
}

// rankedKeys returns map keys ordered by count descending, then key ascending.
//
// The name tiebreak is not cosmetic. Go randomises map iteration order, so
// without a total order the array reshuffles on every request: chips jump
// around between renders, HTTP caching is defeated, and any test asserting the
// output is flaky. Sorting by count alone is not enough, because equal counts
// are common.
func rankedKeys(counts map[string]int) []string {
	if len(counts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

// topKey returns the highest-count key, ties broken by name ascending so the
// result is deterministic. Empty string when there is nothing to report.
func topKey(counts map[string]int) string {
	ranked := rankedKeys(counts)
	if len(ranked) == 0 {
		return ""
	}
	return ranked[0]
}

// NewRelayView derives the wire representation from a cache entry.
func NewRelayView(e cache.RelayEntry) RelayView {
	return RelayView{
		RelayEntry:       e,
		Topics:           rankedKeys(e.Topics),
		TopicCounts:      e.Topics,
		Atmosphere:       topKey(e.Atmosphere),
		AtmosphereCounts: e.Atmosphere,
	}
}

// NewRelayViews derives views for a slice, preserving order.
func NewRelayViews(entries []cache.RelayEntry) []RelayView {
	if entries == nil {
		return nil
	}
	views := make([]RelayView, 0, len(entries))
	for _, e := range entries {
		views = append(views, NewRelayView(e))
	}
	return views
}
