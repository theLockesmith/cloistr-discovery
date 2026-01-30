// Package cache provides the Dragonfly/Redis cache interface for discovery indexing.
// Dragonfly was chosen over Redis for 80% better memory efficiency with a compatible API.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
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
// relay:{url}              -> Kind 30069 JSON (relay directory entry)
// relay:health:{url}       -> "online"/"degraded"/"offline"
// inventory:{relay}:{pk}   -> "1" (relay has pubkey's content)
// pubkey:{pk}:relays       -> Set of relay URLs
// activity:{pk}            -> Kind 30067 JSON
// streams:active           -> Set of event IDs
// relays:by:nip:{nip}      -> Set of relay URLs
// relays:by:location:{cc}  -> Set of relay URLs

// RelayEntry represents a cached relay directory entry (Kind 30069).
type RelayEntry struct {
	URL            string    `json:"url"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Pubkey         string    `json:"pubkey"`
	SupportedNIPs  []int     `json:"supported_nips"`
	Software       string    `json:"software"`
	Version        string    `json:"version"`
	Health         string    `json:"health"` // online, degraded, offline
	LatencyMs      int       `json:"latency_ms"`
	LastChecked    time.Time `json:"last_checked"`
	CountryCode    string    `json:"country_code,omitempty"`
	PaymentRequired bool      `json:"payment_required"`
	AuthRequired   bool      `json:"auth_required"`
}

// SetRelayEntry caches a relay directory entry.
func (c *Client) SetRelayEntry(ctx context.Context, entry *RelayEntry, ttl time.Duration) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal relay entry: %w", err)
	}

	key := "relay:" + entry.URL
	if err := c.rdb.Set(ctx, key, data, ttl).Err(); err != nil {
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

	return nil
}

// GetRelayEntry retrieves a relay directory entry.
func (c *Client) GetRelayEntry(ctx context.Context, url string) (*RelayEntry, error) {
	key := "relay:" + url
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get relay entry: %w", err)
	}

	var entry RelayEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal relay entry: %w", err)
	}

	return &entry, nil
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
