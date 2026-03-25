// Package cache provides the Dragonfly/Redis cache interface for discovery indexing.
// Dragonfly was chosen over Redis for 80% better memory efficiency with a compatible API.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"git.coldforge.xyz/coldforge/cloistr-discovery/internal/metrics"
)

// TTL constants for cache entries.
const (
	// RelayEntryTTL is the TTL for relay directory entries.
	RelayEntryTTL = time.Hour

	// RelayHealthTTL is the TTL for relay health status.
	// This should be shorter than RelayEntryTTL since health changes more frequently.
	RelayHealthTTL = 10 * time.Minute

	// RelayIndexTTL is the TTL for relay index entries (by NIP, location, etc.).
	// Matches RelayEntryTTL to ensure indexes stay in sync with entries.
	RelayIndexTTL = time.Hour

	// UserNIP65TTL is the TTL for cached user NIP-65 relay lists.
	// Short TTL since users may update their relay lists.
	UserNIP65TTL = 5 * time.Minute

	// RelayPrefsTTL is the TTL for cached relay preferences.
	// Short TTL for cloistr-common library fast path queries.
	RelayPrefsTTL = 5 * time.Minute

	// UserContactsTTL is the TTL for cached user contact lists (NIP-02).
	// Longer TTL since contact lists change less frequently.
	UserContactsTTL = 15 * time.Minute

	// WoTRelayScoresTTL is the TTL for cached WoT relay scores.
	// Computed from aggregating follows' relay lists.
	WoTRelayScoresTTL = 10 * time.Minute

	// RelayReviewsTTL is the TTL for cached relay reviews.
	RelayReviewsTTL = 30 * time.Minute
)

// Client wraps the Redis client for discovery caching.
type Client struct {
	rdb *redis.Client
}

// New creates a new cache client from a Redis URL.
// Works with both Redis and Dragonfly (API compatible).
func New(url string) (*Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("invalid cache URL: %w", err)
	}

	rdb := redis.NewClient(opts)
	return &Client{rdb: rdb}, nil
}

// Close closes the cache connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping verifies the cache connection.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Key patterns for discovery data:
// relay:{url}                       -> Kind 30072 JSON (relay directory entry)
// relay:health:{url}                -> "online"/"degraded"/"offline"
// inventory:{relay}:{pk}            -> "1" (relay has pubkey's content)
// pubkey:{pk}:relays                -> Set of relay URLs
// activity:{pk}                     -> Kind 30070 JSON
// streams:active                    -> Set of event IDs
// relays:by:nip:{nip}               -> Set of relay URLs
// relays:by:location:{cc}           -> Set of relay URLs
// relays:by:topic:{topic}           -> Set of relay URLs
// relays:by:atmosphere:{atm}        -> Set of relay URLs
// relays:by:moderation:{level}      -> Set of relay URLs
// relays:by:content_policy:{policy} -> Set of relay URLs
// relays:by:language:{lang}         -> Set of relay URLs
// relays:by:community:{name}        -> Set of relay URLs
// relay:topics:{url}                -> Map of topic -> count
// relay:atmosphere:{url}            -> Map of atmosphere -> count
// relay:annotations:{url}           -> List of annotation events

// RelayEntry represents a cached relay directory entry (Kind 30072).
type RelayEntry struct {
	URL             string    `json:"url"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Pubkey          string    `json:"pubkey"`
	SupportedNIPs   []int     `json:"supported_nips"`
	Software        string    `json:"software"`
	Version         string    `json:"version"`
	Health          string    `json:"health"` // online, degraded, offline
	LatencyMs       int       `json:"latency_ms"`
	UptimePercent   *float64  `json:"uptime_percent,omitempty"` // uptime percentage over last 24h
	LastChecked     time.Time `json:"last_checked"`
	CountryCode     string    `json:"country_code,omitempty"`
	PaymentRequired bool      `json:"payment_required"`
	AuthRequired    bool      `json:"auth_required"`

	// Community & segregation metadata
	ContentPolicy    string            `json:"content_policy,omitempty"`    // anything, sfw, nsfw-allowed, nsfw-only
	Moderation       string            `json:"moderation,omitempty"`        // unmoderated, light, active, strict
	ModerationPolicy string            `json:"moderation_policy,omitempty"` // URL to relay rules
	Community        string            `json:"community,omitempty"`         // freeform community name
	Languages        []string          `json:"languages,omitempty"`         // ISO 639-1 codes
	Topics           map[string]int    `json:"topics,omitempty"`            // topic -> annotation count
	Atmosphere       map[string]int    `json:"atmosphere,omitempty"`        // atmosphere -> annotation count

	// External monitor reports (from NIP-66 events)
	ExternalMonitors []ExternalMonitorReport `json:"external_monitors,omitempty"`
}

// ExternalMonitorReport represents health data from another NIP-66 monitor.
type ExternalMonitorReport struct {
	MonitorPubkey string    `json:"monitor_pubkey"`
	LatencyMs     int       `json:"latency_ms,omitempty"`
	LastSeen      time.Time `json:"last_seen"`
}

// SetRelayEntry caches a relay directory entry.
func (c *Client) SetRelayEntry(ctx context.Context, entry *RelayEntry, ttl time.Duration) error {
	metrics.CacheOperationsTotal.WithLabelValues("set_relay").Inc()

	data, err := json.Marshal(entry)
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_relay").Inc()
		return fmt.Errorf("failed to marshal relay entry: %w", err)
	}

	key := "relay:" + entry.URL
	if err := c.rdb.Set(ctx, key, data, ttl).Err(); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_relay").Inc()
		return fmt.Errorf("failed to set relay entry: %w", err)
	}

	// Update health separately for quick lookups
	healthKey := "relay:health:" + entry.URL
	if err := c.rdb.Set(ctx, healthKey, entry.Health, RelayHealthTTL).Err(); err != nil {
		return fmt.Errorf("failed to set relay health: %w", err)
	}

	// Index by supported NIPs
	for _, nip := range entry.SupportedNIPs {
		nipKey := fmt.Sprintf("relays:by:nip:%d", nip)
		c.rdb.SAdd(ctx, nipKey, entry.URL)
		c.rdb.Expire(ctx, nipKey, ttl)
	}

	// Index by country if available
	if entry.CountryCode != "" {
		locKey := "relays:by:location:" + entry.CountryCode
		c.rdb.SAdd(ctx, locKey, entry.URL)
		c.rdb.Expire(ctx, locKey, ttl)
	}

	// Index by topics (from annotation aggregation)
	for topic := range entry.Topics {
		topicKey := "relays:by:topic:" + topic
		c.rdb.SAdd(ctx, topicKey, entry.URL)
		c.rdb.Expire(ctx, topicKey, ttl)
	}

	// Index by atmosphere (from annotation aggregation)
	for atm := range entry.Atmosphere {
		atmKey := "relays:by:atmosphere:" + atm
		c.rdb.SAdd(ctx, atmKey, entry.URL)
		c.rdb.Expire(ctx, atmKey, ttl)
	}

	// Index by moderation level
	if entry.Moderation != "" {
		modKey := "relays:by:moderation:" + entry.Moderation
		c.rdb.SAdd(ctx, modKey, entry.URL)
		c.rdb.Expire(ctx, modKey, ttl)
	}

	// Index by content policy
	if entry.ContentPolicy != "" {
		cpKey := "relays:by:content_policy:" + entry.ContentPolicy
		c.rdb.SAdd(ctx, cpKey, entry.URL)
		c.rdb.Expire(ctx, cpKey, ttl)
	}

	// Index by languages
	for _, lang := range entry.Languages {
		langKey := "relays:by:language:" + lang
		c.rdb.SAdd(ctx, langKey, entry.URL)
		c.rdb.Expire(ctx, langKey, ttl)
	}

	// Index by community name
	if entry.Community != "" {
		commKey := "relays:by:community:" + entry.Community
		c.rdb.SAdd(ctx, commKey, entry.URL)
		c.rdb.Expire(ctx, commKey, ttl)
	}

	return nil
}

// GetRelayEntry retrieves a relay directory entry.
func (c *Client) GetRelayEntry(ctx context.Context, url string) (*RelayEntry, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_relay").Inc()

	key := "relay:" + url
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relay").Inc()
		return nil, fmt.Errorf("failed to get relay entry: %w", err)
	}

	var entry RelayEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relay").Inc()
		return nil, fmt.Errorf("failed to unmarshal relay entry: %w", err)
	}

	return &entry, nil
}

// GetRelayEntriesBatch retrieves multiple relay entries in a single round-trip using pipelining.
// Returns entries in the same order as the input URLs. Missing entries are returned as nil.
func (c *Client) GetRelayEntriesBatch(ctx context.Context, urls []string) ([]*RelayEntry, error) {
	if len(urls) == 0 {
		return nil, nil
	}

	metrics.CacheOperationsTotal.WithLabelValues("get_relay_batch").Inc()

	// Build keys and execute pipeline
	pipe := c.rdb.Pipeline()
	cmds := make([]*redis.StringCmd, len(urls))
	for i, url := range urls {
		cmds[i] = pipe.Get(ctx, "relay:"+url)
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relay_batch").Inc()
		return nil, fmt.Errorf("failed to execute batch get: %w", err)
	}

	// Process results
	entries := make([]*RelayEntry, len(urls))
	for i, cmd := range cmds {
		data, err := cmd.Bytes()
		if err == redis.Nil {
			continue // Entry not found, leave as nil
		}
		if err != nil {
			continue // Skip individual errors
		}

		var entry RelayEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue // Skip malformed entries
		}
		entries[i] = &entry
	}

	return entries, nil
}

// GetRelaysByNIP returns all relays supporting a specific NIP.
func (c *Client) GetRelaysByNIP(ctx context.Context, nip int) ([]string, error) {
	key := fmt.Sprintf("relays:by:nip:%d", nip)
	return c.rdb.SMembers(ctx, key).Result()
}

// GetRelaysByLocation returns all relays in a specific country.
func (c *Client) GetRelaysByLocation(ctx context.Context, countryCode string) ([]string, error) {
	key := "relays:by:location:" + countryCode
	return c.rdb.SMembers(ctx, key).Result()
}

// GetRelaysByTopic returns all relays tagged with a specific topic.
func (c *Client) GetRelaysByTopic(ctx context.Context, topic string) ([]string, error) {
	key := "relays:by:topic:" + topic
	return c.rdb.SMembers(ctx, key).Result()
}

// GetRelaysByAtmosphere returns all relays tagged with a specific atmosphere.
func (c *Client) GetRelaysByAtmosphere(ctx context.Context, atmosphere string) ([]string, error) {
	key := "relays:by:atmosphere:" + atmosphere
	return c.rdb.SMembers(ctx, key).Result()
}

// GetRelaysByModeration returns all relays with a specific moderation level.
func (c *Client) GetRelaysByModeration(ctx context.Context, level string) ([]string, error) {
	key := "relays:by:moderation:" + level
	return c.rdb.SMembers(ctx, key).Result()
}

// GetRelaysByContentPolicy returns all relays with a specific content policy.
func (c *Client) GetRelaysByContentPolicy(ctx context.Context, policy string) ([]string, error) {
	key := "relays:by:content_policy:" + policy
	return c.rdb.SMembers(ctx, key).Result()
}

// GetRelaysByLanguage returns all relays supporting a specific language.
func (c *Client) GetRelaysByLanguage(ctx context.Context, lang string) ([]string, error) {
	key := "relays:by:language:" + lang
	return c.rdb.SMembers(ctx, key).Result()
}

// GetRelaysByCommunity returns all relays in a specific community.
func (c *Client) GetRelaysByCommunity(ctx context.Context, community string) ([]string, error) {
	key := "relays:by:community:" + community
	return c.rdb.SMembers(ctx, key).Result()
}

// GetRelaysByOperator returns all relays operated by a specific pubkey.
// Scans all relay entries and filters by operator pubkey.
func (c *Client) GetRelaysByOperator(ctx context.Context, pubkey string) ([]*RelayEntry, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_relays_by_operator").Inc()

	// Get all relay URLs
	urls, err := c.GetAllRelayURLs(ctx)
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relays_by_operator").Inc()
		return nil, fmt.Errorf("failed to get relay URLs: %w", err)
	}

	if len(urls) == 0 {
		return nil, nil
	}

	// Batch fetch all relay entries
	entries, err := c.GetRelayEntriesBatch(ctx, urls)
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relays_by_operator").Inc()
		return nil, fmt.Errorf("failed to fetch relay entries: %w", err)
	}

	// Filter by operator pubkey
	var result []*RelayEntry
	for _, entry := range entries {
		if entry != nil && entry.Pubkey == pubkey {
			result = append(result, entry)
		}
	}

	return result, nil
}

// SetPubkeyRelay records that a relay has content from a pubkey.
func (c *Client) SetPubkeyRelay(ctx context.Context, pubkey, relayURL string, ttl time.Duration) error {
	// Add to pubkey's relay set
	pkKey := "pubkey:" + pubkey + ":relays"
	if err := c.rdb.SAdd(ctx, pkKey, relayURL).Err(); err != nil {
		return fmt.Errorf("failed to add relay to pubkey set: %w", err)
	}
	c.rdb.Expire(ctx, pkKey, ttl)

	// Also set individual inventory marker
	invKey := fmt.Sprintf("inventory:%s:%s", relayURL, pubkey)
	return c.rdb.Set(ctx, invKey, "1", ttl).Err()
}

// GetPubkeyRelays returns all relays that have content from a pubkey.
func (c *Client) GetPubkeyRelays(ctx context.Context, pubkey string) ([]string, error) {
	key := "pubkey:" + pubkey + ":relays"
	return c.rdb.SMembers(ctx, key).Result()
}

// Activity represents a cached activity announcement (Kind 30067).
type Activity struct {
	Pubkey    string    `json:"pubkey"`
	Type      string    `json:"type"` // streaming, online, writing, etc.
	Details   string    `json:"details,omitempty"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SetActivity caches an activity announcement.
func (c *Client) SetActivity(ctx context.Context, activity *Activity, ttl time.Duration) error {
	data, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("failed to marshal activity: %w", err)
	}

	key := "activity:" + activity.Pubkey
	if err := c.rdb.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set activity: %w", err)
	}

	// Track streams separately for quick listing
	if activity.Type == "streaming" {
		c.rdb.SAdd(ctx, "streams:active", activity.Pubkey)
		c.rdb.Expire(ctx, "streams:active", ttl)
	}

	return nil
}

// GetActivity retrieves an activity announcement.
func (c *Client) GetActivity(ctx context.Context, pubkey string) (*Activity, error) {
	key := "activity:" + pubkey
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get activity: %w", err)
	}

	var activity Activity
	if err := json.Unmarshal(data, &activity); err != nil {
		return nil, fmt.Errorf("failed to unmarshal activity: %w", err)
	}

	return &activity, nil
}

// GetActiveStreams returns all pubkeys with active streams.
func (c *Client) GetActiveStreams(ctx context.Context) ([]string, error) {
	return c.rdb.SMembers(ctx, "streams:active").Result()
}

// ClearActivity removes an activity announcement.
func (c *Client) ClearActivity(ctx context.Context, pubkey string) error {
	key := "activity:" + pubkey
	c.rdb.SRem(ctx, "streams:active", pubkey)
	return c.rdb.Del(ctx, key).Err()
}

// Admin key patterns:
// admin:whitelist         -> Set of relay URLs (always monitor)
// admin:blacklist         -> Set of relay URLs (never monitor)
// admin:peers             -> Set of trusted discovery peer pubkeys
// discovery:seen          -> Set of discovered relay URLs (deduplication)
// stats:{name}            -> Counter for various stats

// Whitelist management

// AddToWhitelist adds a relay URL to the whitelist (always monitor).
func (c *Client) AddToWhitelist(ctx context.Context, url string) error {
	return c.rdb.SAdd(ctx, "admin:whitelist", url).Err()
}

// RemoveFromWhitelist removes a relay URL from the whitelist.
func (c *Client) RemoveFromWhitelist(ctx context.Context, url string) error {
	return c.rdb.SRem(ctx, "admin:whitelist", url).Err()
}

// GetWhitelist returns all whitelisted relay URLs.
func (c *Client) GetWhitelist(ctx context.Context) ([]string, error) {
	return c.rdb.SMembers(ctx, "admin:whitelist").Result()
}

// IsWhitelisted checks if a relay URL is whitelisted.
func (c *Client) IsWhitelisted(ctx context.Context, url string) (bool, error) {
	return c.rdb.SIsMember(ctx, "admin:whitelist", url).Result()
}

// Blacklist management

// AddToBlacklist adds a relay URL to the blacklist (never monitor).
func (c *Client) AddToBlacklist(ctx context.Context, url string) error {
	return c.rdb.SAdd(ctx, "admin:blacklist", url).Err()
}

// RemoveFromBlacklist removes a relay URL from the blacklist.
func (c *Client) RemoveFromBlacklist(ctx context.Context, url string) error {
	return c.rdb.SRem(ctx, "admin:blacklist", url).Err()
}

// GetBlacklist returns all blacklisted relay URLs.
func (c *Client) GetBlacklist(ctx context.Context) ([]string, error) {
	return c.rdb.SMembers(ctx, "admin:blacklist").Result()
}

// IsBlacklisted checks if a relay URL is blacklisted.
func (c *Client) IsBlacklisted(ctx context.Context, url string) (bool, error) {
	return c.rdb.SIsMember(ctx, "admin:blacklist", url).Result()
}

// Trusted peers management

// AddTrustedPeer adds a pubkey to the trusted discovery peers list.
func (c *Client) AddTrustedPeer(ctx context.Context, pubkey string) error {
	return c.rdb.SAdd(ctx, "admin:peers", pubkey).Err()
}

// RemoveTrustedPeer removes a pubkey from the trusted discovery peers list.
func (c *Client) RemoveTrustedPeer(ctx context.Context, pubkey string) error {
	return c.rdb.SRem(ctx, "admin:peers", pubkey).Err()
}

// GetTrustedPeers returns all trusted discovery peer pubkeys.
func (c *Client) GetTrustedPeers(ctx context.Context) ([]string, error) {
	return c.rdb.SMembers(ctx, "admin:peers").Result()
}

// Discovery deduplication

// MarkRelaySeen marks a relay URL as seen and returns true if it was newly seen.
func (c *Client) MarkRelaySeen(ctx context.Context, url string) (bool, error) {
	// SAdd returns 1 if the element was added, 0 if it already existed
	result, err := c.rdb.SAdd(ctx, "discovery:seen", url).Result()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// GetSeenRelays returns all seen relay URLs.
func (c *Client) GetSeenRelays(ctx context.Context) ([]string, error) {
	return c.rdb.SMembers(ctx, "discovery:seen").Result()
}

// ClearSeenRelays clears the seen relays set.
func (c *Client) ClearSeenRelays(ctx context.Context) error {
	return c.rdb.Del(ctx, "discovery:seen").Err()
}

// Stats management

// IncrementStat increments a stats counter.
func (c *Client) IncrementStat(ctx context.Context, stat string) error {
	return c.rdb.Incr(ctx, "stats:"+stat).Err()
}

// DecrementStat decrements a stats counter.
func (c *Client) DecrementStat(ctx context.Context, stat string) error {
	return c.rdb.Decr(ctx, "stats:"+stat).Err()
}

// GetStat retrieves a stats counter value.
func (c *Client) GetStat(ctx context.Context, stat string) (int64, error) {
	val, err := c.rdb.Get(ctx, "stats:"+stat).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

// SetStat sets a stats counter to a specific value.
func (c *Client) SetStat(ctx context.Context, stat string, value int64) error {
	return c.rdb.Set(ctx, "stats:"+stat, value, 0).Err()
}

// GetAllStats retrieves all stats counters.
func (c *Client) GetAllStats(ctx context.Context) (map[string]int64, error) {
	// Scan for all stats keys
	var cursor uint64
	stats := make(map[string]int64)

	for {
		keys, nextCursor, err := c.rdb.Scan(ctx, cursor, "stats:*", 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			val, err := c.rdb.Get(ctx, key).Int64()
			if err != nil && err != redis.Nil {
				continue
			}
			// Strip "stats:" prefix
			statName := key[6:]
			stats[statName] = val
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return stats, nil
}

// GetAllRelayURLs returns all relay URLs currently being monitored.
func (c *Client) GetAllRelayURLs(ctx context.Context) ([]string, error) {
	// Scan for all relay keys
	var cursor uint64
	var urls []string

	for {
		keys, nextCursor, err := c.rdb.Scan(ctx, cursor, "relay:wss://*", 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			// Skip health keys
			if len(key) > 13 && key[6:13] == "health:" {
				continue
			}
			// Strip "relay:" prefix
			url := key[6:]
			urls = append(urls, url)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return urls, nil
}

// UserNIP65Entry represents a cached NIP-65 relay list for a user.
// Key pattern: user:nip65:{pubkey}
type UserNIP65Entry struct {
	Pubkey    string          `json:"pubkey"`
	Relays    []UserRelayData `json:"relays"`
	FetchedAt time.Time       `json:"fetched_at"`
}

// UserRelayData represents a relay from a user's NIP-65 event.
type UserRelayData struct {
	URL   string `json:"url"`
	Read  bool   `json:"read"`
	Write bool   `json:"write"`
}

// SetUserNIP65 caches a user's NIP-65 relay list.
func (c *Client) SetUserNIP65(ctx context.Context, entry *UserNIP65Entry) error {
	metrics.CacheOperationsTotal.WithLabelValues("set_user_nip65").Inc()

	data, err := json.Marshal(entry)
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_user_nip65").Inc()
		return fmt.Errorf("failed to marshal user NIP-65 entry: %w", err)
	}

	key := "user:nip65:" + entry.Pubkey
	if err := c.rdb.Set(ctx, key, data, UserNIP65TTL).Err(); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_user_nip65").Inc()
		return fmt.Errorf("failed to set user NIP-65 entry: %w", err)
	}

	return nil
}

// GetUserNIP65 retrieves a user's cached NIP-65 relay list.
// Returns nil, nil if not cached.
func (c *Client) GetUserNIP65(ctx context.Context, pubkey string) (*UserNIP65Entry, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_user_nip65").Inc()

	key := "user:nip65:" + pubkey
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_user_nip65").Inc()
		return nil, fmt.Errorf("failed to get user NIP-65 entry: %w", err)
	}

	var entry UserNIP65Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_user_nip65").Inc()
		return nil, fmt.Errorf("failed to unmarshal user NIP-65 entry: %w", err)
	}

	return &entry, nil
}

// RelayPrefsEntry represents cached relay preferences for a user.
// Key pattern: relayprefs:{pubkey}
// Source is "cloistr-relays" (kind:30078), "nip65" (kind:10002), or "default".
type RelayPrefsEntry struct {
	Pubkey   string          `json:"pubkey"`
	Relays   []UserRelayData `json:"relays"`
	Source   string          `json:"source"`
	CachedAt time.Time       `json:"cached_at"`
}

// SetRelayPrefs caches a user's relay preferences.
func (c *Client) SetRelayPrefs(ctx context.Context, entry *RelayPrefsEntry) error {
	metrics.CacheOperationsTotal.WithLabelValues("set_relay_prefs").Inc()

	data, err := json.Marshal(entry)
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_relay_prefs").Inc()
		return fmt.Errorf("failed to marshal relay prefs entry: %w", err)
	}

	key := "relayprefs:" + entry.Pubkey
	if err := c.rdb.Set(ctx, key, data, RelayPrefsTTL).Err(); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_relay_prefs").Inc()
		return fmt.Errorf("failed to set relay prefs entry: %w", err)
	}

	return nil
}

// GetRelayPrefs retrieves a user's cached relay preferences.
// Returns nil, nil if not cached.
func (c *Client) GetRelayPrefs(ctx context.Context, pubkey string) (*RelayPrefsEntry, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_relay_prefs").Inc()

	key := "relayprefs:" + pubkey
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relay_prefs").Inc()
		return nil, fmt.Errorf("failed to get relay prefs entry: %w", err)
	}

	var entry RelayPrefsEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relay_prefs").Inc()
		return nil, fmt.Errorf("failed to unmarshal relay prefs entry: %w", err)
	}

	return &entry, nil
}

// UserContactsEntry represents a cached NIP-02 contact list for a user.
// Key pattern: user:contacts:{pubkey}
type UserContactsEntry struct {
	Pubkey    string    `json:"pubkey"`
	Follows   []string  `json:"follows"` // List of followed pubkeys
	FetchedAt time.Time `json:"fetched_at"`
}

// SetUserContacts caches a user's NIP-02 contact list.
func (c *Client) SetUserContacts(ctx context.Context, entry *UserContactsEntry) error {
	metrics.CacheOperationsTotal.WithLabelValues("set_user_contacts").Inc()

	data, err := json.Marshal(entry)
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_user_contacts").Inc()
		return fmt.Errorf("failed to marshal user contacts entry: %w", err)
	}

	key := "user:contacts:" + entry.Pubkey
	if err := c.rdb.Set(ctx, key, data, UserContactsTTL).Err(); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_user_contacts").Inc()
		return fmt.Errorf("failed to set user contacts entry: %w", err)
	}

	return nil
}

// GetUserContacts retrieves a user's cached NIP-02 contact list.
// Returns nil, nil if not cached.
func (c *Client) GetUserContacts(ctx context.Context, pubkey string) (*UserContactsEntry, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_user_contacts").Inc()

	key := "user:contacts:" + pubkey
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_user_contacts").Inc()
		return nil, fmt.Errorf("failed to get user contacts entry: %w", err)
	}

	var entry UserContactsEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_user_contacts").Inc()
		return nil, fmt.Errorf("failed to unmarshal user contacts entry: %w", err)
	}

	return &entry, nil
}

// WoTRelayScoresEntry represents cached WoT relay scores for a user.
// Key pattern: wot:scores:{pubkey}
type WoTRelayScoresEntry struct {
	Pubkey       string         `json:"pubkey"`
	RelayScores  map[string]int `json:"relay_scores"`  // relay URL -> score (number of follows using it)
	FollowsCount int            `json:"follows_count"` // Total follows analyzed
	ComputedAt   time.Time      `json:"computed_at"`
}

// SetWoTRelayScores caches computed WoT relay scores for a user.
func (c *Client) SetWoTRelayScores(ctx context.Context, entry *WoTRelayScoresEntry) error {
	metrics.CacheOperationsTotal.WithLabelValues("set_wot_scores").Inc()

	data, err := json.Marshal(entry)
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_wot_scores").Inc()
		return fmt.Errorf("failed to marshal WoT scores entry: %w", err)
	}

	key := "wot:scores:" + entry.Pubkey
	if err := c.rdb.Set(ctx, key, data, WoTRelayScoresTTL).Err(); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_wot_scores").Inc()
		return fmt.Errorf("failed to set WoT scores entry: %w", err)
	}

	return nil
}

// GetWoTRelayScores retrieves cached WoT relay scores for a user.
// Returns nil, nil if not cached.
func (c *Client) GetWoTRelayScores(ctx context.Context, pubkey string) (*WoTRelayScoresEntry, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_wot_scores").Inc()

	key := "wot:scores:" + pubkey
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_wot_scores").Inc()
		return nil, fmt.Errorf("failed to get WoT scores entry: %w", err)
	}

	var entry WoTRelayScoresEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_wot_scores").Inc()
		return nil, fmt.Errorf("failed to unmarshal WoT scores entry: %w", err)
	}

	return &entry, nil
}

// RelayReview represents a single review of a relay.
type RelayReview struct {
	Pubkey    string    `json:"pubkey"`     // Reviewer's pubkey
	Rating    int       `json:"rating"`     // 1-5 stars
	Comment   string    `json:"comment"`    // Optional review text
	CreatedAt time.Time `json:"created_at"` // When review was created
}

// RelayReviewsEntry represents cached reviews for a relay.
// Key pattern: relay:reviews:{url}
type RelayReviewsEntry struct {
	RelayURL      string        `json:"relay_url"`
	Reviews       []RelayReview `json:"reviews"`
	AverageRating float64       `json:"average_rating"`
	TotalReviews  int           `json:"total_reviews"`
	FetchedAt     time.Time     `json:"fetched_at"`
}

// SetRelayReviews caches reviews for a relay.
func (c *Client) SetRelayReviews(ctx context.Context, entry *RelayReviewsEntry) error {
	metrics.CacheOperationsTotal.WithLabelValues("set_relay_reviews").Inc()

	data, err := json.Marshal(entry)
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_relay_reviews").Inc()
		return fmt.Errorf("failed to marshal relay reviews entry: %w", err)
	}

	key := "relay:reviews:" + entry.RelayURL
	if err := c.rdb.Set(ctx, key, data, RelayReviewsTTL).Err(); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("set_relay_reviews").Inc()
		return fmt.Errorf("failed to set relay reviews entry: %w", err)
	}

	return nil
}

// GetRelayReviews retrieves cached reviews for a relay.
// Returns nil, nil if not cached.
func (c *Client) GetRelayReviews(ctx context.Context, relayURL string) (*RelayReviewsEntry, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_relay_reviews").Inc()

	key := "relay:reviews:" + relayURL
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relay_reviews").Inc()
		return nil, fmt.Errorf("failed to get relay reviews entry: %w", err)
	}

	var entry RelayReviewsEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_relay_reviews").Inc()
		return nil, fmt.Errorf("failed to unmarshal relay reviews entry: %w", err)
	}

	return &entry, nil
}

// RecordHealthCheck records a health check result for uptime tracking.
// Uses a sorted set with timestamp as score and success/failure as member.
// Key pattern: relay:health:history:{url}
func (c *Client) RecordHealthCheck(ctx context.Context, relayURL string, success bool, timestamp time.Time) error {
	metrics.CacheOperationsTotal.WithLabelValues("record_health_check").Inc()

	key := "relay:health:history:" + relayURL
	score := float64(timestamp.Unix())

	// Store "1" for successful check, "0" for failed
	member := "0"
	if success {
		member = "1"
	}

	// Add the health check result
	if err := c.rdb.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: member + ":" + fmt.Sprintf("%d", timestamp.Unix()),
	}).Err(); err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("record_health_check").Inc()
		return fmt.Errorf("failed to record health check: %w", err)
	}

	// Keep only last 30 days of data
	cutoff := float64(time.Now().Add(-30 * 24 * time.Hour).Unix())
	if err := c.rdb.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%f", cutoff)).Err(); err != nil {
		// Log but don't fail on cleanup error
		fmt.Printf("warning: failed to clean old health checks for %s: %v\n", relayURL, err)
	}

	// Set TTL to 31 days (slightly longer than our window)
	c.rdb.Expire(ctx, key, 31*24*time.Hour)

	return nil
}

// GetUptimePercent calculates uptime percentage from health check history.
// Returns nil if there's insufficient data (less than 2 checks).
func (c *Client) GetUptimePercent(ctx context.Context, relayURL string, window time.Duration) (*float64, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_uptime_percent").Inc()

	key := "relay:health:history:" + relayURL

	// Get all checks within the time window
	cutoff := float64(time.Now().Add(-window).Unix())
	checks, err := c.rdb.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", cutoff),
		Max: "+inf",
	}).Result()

	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_uptime_percent").Inc()
		return nil, fmt.Errorf("failed to get health check history: %w", err)
	}

	// Need at least 2 checks to calculate meaningful uptime
	if len(checks) < 2 {
		return nil, nil
	}

	// Count successful checks
	successCount := 0
	for _, check := range checks {
		// Check format is "1:timestamp" or "0:timestamp"
		if len(check) > 0 && check[0] == '1' {
			successCount++
		}
	}

	uptime := float64(successCount) / float64(len(checks)) * 100.0
	return &uptime, nil
}

// UptimeStats contains uptime percentages and check counts for multiple time windows.
type UptimeStats struct {
	Uptime24h     *float64 `json:"uptime_24h,omitempty"`
	Uptime7d      *float64 `json:"uptime_7d,omitempty"`
	Uptime30d     *float64 `json:"uptime_30d,omitempty"`
	CheckCount24h int      `json:"check_count_24h"`
	CheckCount7d  int      `json:"check_count_7d"`
	CheckCount30d int      `json:"check_count_30d"`
}

// GetUptimeStats returns uptime percentages for 24h, 7d, and 30d windows.
func (c *Client) GetUptimeStats(ctx context.Context, relayURL string) (*UptimeStats, error) {
	metrics.CacheOperationsTotal.WithLabelValues("get_uptime_stats").Inc()

	key := "relay:health:history:" + relayURL

	// Get all checks from the last 30 days
	cutoff30d := float64(time.Now().Add(-30 * 24 * time.Hour).Unix())
	checks, err := c.rdb.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", cutoff30d),
		Max: "+inf",
	}).Result()

	if err != nil {
		metrics.CacheErrorsTotal.WithLabelValues("get_uptime_stats").Inc()
		return nil, fmt.Errorf("failed to get health check history: %w", err)
	}

	stats := &UptimeStats{}

	if len(checks) == 0 {
		return stats, nil
	}

	// Time boundaries
	now := time.Now()
	cutoff24h := now.Add(-24 * time.Hour).Unix()
	cutoff7d := now.Add(-7 * 24 * time.Hour).Unix()

	// Count successes for each window
	var success24h, total24h, success7d, total7d, success30d, total30d int

	for _, z := range checks {
		timestamp := int64(z.Score)
		member := z.Member.(string)
		isSuccess := len(member) > 0 && member[0] == '1'

		// 30-day window (all checks)
		total30d++
		if isSuccess {
			success30d++
		}

		// 7-day window
		if timestamp >= cutoff7d {
			total7d++
			if isSuccess {
				success7d++
			}
		}

		// 24-hour window
		if timestamp >= cutoff24h {
			total24h++
			if isSuccess {
				success24h++
			}
		}
	}

	stats.CheckCount24h = total24h
	stats.CheckCount7d = total7d
	stats.CheckCount30d = total30d

	// Calculate percentages (require at least 2 checks for meaningful data)
	if total24h >= 2 {
		uptime := float64(success24h) / float64(total24h) * 100.0
		stats.Uptime24h = &uptime
	}
	if total7d >= 2 {
		uptime := float64(success7d) / float64(total7d) * 100.0
		stats.Uptime7d = &uptime
	}
	if total30d >= 2 {
		uptime := float64(success30d) / float64(total30d) * 100.0
		stats.Uptime30d = &uptime
	}

	return stats, nil
}
