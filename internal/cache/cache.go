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
	if err := c.rdb.Set(ctx, healthKey, entry.Health, 5*time.Minute).Err(); err != nil {
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
