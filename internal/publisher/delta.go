package publisher

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"
	"time"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

type deltaTracker struct {
	mu              sync.RWMutex
	hashes          map[string]string
	lastFullPublish time.Time
}

func newDeltaTracker() *deltaTracker {
	return &deltaTracker{
		hashes: make(map[string]string),
	}
}

func (d *deltaTracker) changed(url string, entry *cache.RelayEntry) bool {
	hash := hashRelayData(entry)
	d.mu.RLock()
	prev, exists := d.hashes[url]
	d.mu.RUnlock()
	return !exists || prev != hash
}

func (d *deltaTracker) record(url string, entry *cache.RelayEntry) {
	hash := hashRelayData(entry)
	d.mu.Lock()
	d.hashes[url] = hash
	d.mu.Unlock()
}

func (d *deltaTracker) needsFullRefresh(interval time.Duration) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastFullPublish.IsZero() || time.Since(d.lastFullPublish) >= interval
}

func (d *deltaTracker) markFullRefresh() {
	d.mu.Lock()
	d.lastFullPublish = time.Now()
	d.mu.Unlock()
}

func (d *deltaTracker) prune(activeURLs map[string]bool) {
	d.mu.Lock()
	for url := range d.hashes {
		if !activeURLs[url] {
			delete(d.hashes, url)
		}
	}
	d.mu.Unlock()
}

// hashRelayData produces a deterministic hash of the fields that go into a
// published event. Volatile fields (LastChecked) are excluded and latency
// is quantized to 50 ms buckets so minor jitter does not force a republish.
func hashRelayData(entry *cache.RelayEntry) string {
	h := sha256.New()
	w := func(format string, args ...interface{}) {
		_, _ = fmt.Fprintf(h, format, args...)
	}
	w("url=%s\n", entry.URL)
	w("name=%s\n", entry.Name)
	w("desc=%s\n", entry.Description)
	w("pk=%s\n", entry.Pubkey)
	w("sw=%s\n", entry.Software)
	w("ver=%s\n", entry.Version)
	w("health=%s\n", entry.Health)
	w("lat=%d\n", entry.LatencyMs/50)
	w("cc=%s\n", entry.CountryCode)
	w("pay=%v\n", entry.PaymentRequired)
	w("auth=%v\n", entry.AuthRequired)
	w("cp=%s\n", entry.ContentPolicy)
	w("mod=%s\n", entry.Moderation)
	w("modp=%s\n", entry.ModerationPolicy)
	w("comm=%s\n", entry.Community)

	nips := make([]int, len(entry.SupportedNIPs))
	copy(nips, entry.SupportedNIPs)
	sort.Ints(nips)
	for _, n := range nips {
		w("nip=%d\n", n)
	}

	langs := make([]string, len(entry.Languages))
	copy(langs, entry.Languages)
	sort.Strings(langs)
	for _, l := range langs {
		w("lang=%s\n", l)
	}

	topicKeys := make([]string, 0, len(entry.Topics))
	for k := range entry.Topics {
		topicKeys = append(topicKeys, k)
	}
	sort.Strings(topicKeys)
	for _, k := range topicKeys {
		w("topic=%s:%d\n", k, entry.Topics[k])
	}

	atmKeys := make([]string, 0, len(entry.Atmosphere))
	for k := range entry.Atmosphere {
		atmKeys = append(atmKeys, k)
	}
	sort.Strings(atmKeys)
	for _, k := range atmKeys {
		w("atm=%s:%d\n", k, entry.Atmosphere[k])
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
