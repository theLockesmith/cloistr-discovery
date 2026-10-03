package publisher

import (
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

// canonicalizeRelayURL normalizes a relay URL: lowercase scheme+host,
// drop default port, trailing slash, query and fragment. Keeps the path.
func canonicalizeRelayURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	u.Host = trimHostDot(u)
	port := u.Port()
	if (u.Scheme == "wss" && port == "443") || (u.Scheme == "ws" && port == "80") {
		u.Host = u.Hostname()
	}

	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""

	return u.String()
}

// hostKey returns scheme+host (no path) for grouping entries by host.
func hostKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	u.Host = trimHostDot(u)
	port := u.Port()
	if (u.Scheme == "wss" && port == "443") || (u.Scheme == "ws" && port == "80") {
		u.Host = u.Hostname()
	}

	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""

	return u.String()
}

// trimHostDot drops a trailing DNS root dot ("relay.example.com.") so the
// fully-qualified form groups with the plain hostname.
func trimHostDot(u *url.URL) string {
	host := strings.TrimSuffix(u.Hostname(), ".")
	if port := u.Port(); port != "" {
		return net.JoinHostPort(host, port)
	}
	return host
}

func hasPath(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	p := strings.TrimRight(u.Path, "/")
	return p != ""
}

func hasNIP11(entry *cache.RelayEntry) bool {
	return entry.Name != "" || entry.Software != "" || len(entry.SupportedNIPs) > 0
}

func nip11Fingerprint(entry *cache.RelayEntry) string {
	parts := []string{entry.Name, entry.Software, entry.Version}
	nips := make([]int, len(entry.SupportedNIPs))
	copy(nips, entry.SupportedNIPs)
	sort.Ints(nips)
	for _, n := range nips {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, "|")
}

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

func isBetterCandidate(candidate, current *cache.RelayEntry) bool {
	cRank := healthRank(candidate.Health)
	eRank := healthRank(current.Health)
	if cRank != eRank {
		return cRank < eRank
	}
	return len(candidate.URL) < len(current.URL)
}

// deduplicateByHost groups relay entries by host and collapses path variants
// that are not distinct relays. A path variant is kept only if it has NIP-11
// info that differs from the host root's. Variants without NIP-11 or with
// identical NIP-11 to root are collapsed.
func deduplicateByHost(entries map[string]*cache.RelayEntry) []*cache.RelayEntry {
	groups := make(map[string][]*cache.RelayEntry)
	for _, entry := range entries {
		hk := hostKey(entry.URL)
		groups[hk] = append(groups[hk], entry)
	}

	var result []*cache.RelayEntry
	for _, group := range groups {
		if len(group) == 1 {
			result = append(result, group[0])
			continue
		}

		var root *cache.RelayEntry
		var pathVariants []*cache.RelayEntry
		for _, e := range group {
			if !hasPath(e.URL) {
				if root == nil || healthRank(e.Health) < healthRank(root.Health) {
					root = e
				}
			} else {
				pathVariants = append(pathVariants, e)
			}
		}

		if root != nil {
			result = append(result, root)
		}

		rootFP := ""
		if root != nil {
			rootFP = nip11Fingerprint(root)
		}

		kept := 0
		for _, v := range pathVariants {
			if !hasNIP11(v) {
				continue
			}
			if root != nil && nip11Fingerprint(v) == rootFP {
				continue
			}
			result = append(result, v)
			kept++
		}

		if root == nil && kept == 0 && len(pathVariants) > 0 {
			best := pathVariants[0]
			for _, v := range pathVariants[1:] {
				if isBetterCandidate(v, best) {
					best = v
				}
			}
			result = append(result, best)
		}
	}

	return result
}

// deduplicateURLs takes a list of URLs and a lookup function, returning
// deduplicated entries. Used by both publishers.
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
