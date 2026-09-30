package publisher

import (
	"net/url"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

// normalizeRelayURL strips the path, query, and fragment from a relay URL
// and removes the default port for the scheme (443 for wss, 80 for ws).
func normalizeRelayURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""

	host := u.Hostname()
	port := u.Port()
	if (u.Scheme == "wss" && port == "443") || (u.Scheme == "ws" && port == "80") {
		u.Host = host
	}

	return u.String()
}

// healthRank returns a sort priority for health status (lower is better).
func healthRank(health string) int {
	switch health {
	case "online":
		return 0
	case "degraded":
		return 1
	default:
		return 2
	}
}

// deduplicateByHost groups relay entries by normalized host and picks one
// representative per host. It prefers entries with a root path, then better
// health, then shorter URL.
func deduplicateByHost(entries map[string]*cache.RelayEntry) []*cache.RelayEntry {
	best := make(map[string]*cache.RelayEntry)

	for _, entry := range entries {
		norm := normalizeRelayURL(entry.URL)
		existing, ok := best[norm]
		if !ok {
			best[norm] = entry
			continue
		}
		if isBetterCandidate(entry, existing) {
			best[norm] = entry
		}
	}

	result := make([]*cache.RelayEntry, 0, len(best))
	for _, entry := range best {
		result = append(result, entry)
	}
	return result
}

func isBetterCandidate(candidate, current *cache.RelayEntry) bool {
	candidateHasPath := hasPath(candidate.URL)
	currentHasPath := hasPath(current.URL)

	if !candidateHasPath && currentHasPath {
		return true
	}
	if candidateHasPath && !currentHasPath {
		return false
	}

	cRank := healthRank(candidate.Health)
	eRank := healthRank(current.Health)
	if cRank != eRank {
		return cRank < eRank
	}

	return len(candidate.URL) < len(current.URL)
}

func hasPath(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Path != "" && u.Path != "/"
}

// deduplicateURLs is a convenience wrapper that takes a list of URLs and a
// lookup function, returning deduplicated entries. Used by both publishers.
func deduplicateURLs(urls []string, lookup func(string) *cache.RelayEntry) []*cache.RelayEntry {
	entries := make(map[string]*cache.RelayEntry, len(urls))
	for _, u := range urls {
		entry := lookup(u)
		if entry != nil {
			entries[u] = entry
		}
	}
	return deduplicateByHost(entries)
}

