// Package query handles kind 30068 discovery query events.
// Clients publish queries, we respond with the requested information.
package query

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"

	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
)

// Handler processes kind 30068 discovery queries.
type Handler struct {
	cfg   *config.Config
	cache *cache.Client
	sk    string // hex private key
	pk    string // hex public key

	mu           sync.RWMutex
	queriesHandled int64
	lastQuery    time.Time
}

// New creates a new query handler.
func New(cfg *config.Config, cache *cache.Client) (*Handler, error) {
	h := &Handler{
		cfg:   cfg,
		cache: cache,
	}

	// Parse private key (supports hex or nsec format)
	if cfg.PrivateKey != "" {
		sk, err := parsePrivateKey(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid private key: %w", err)
		}
		h.sk = sk

		// Derive public key
		pk, err := nostr.GetPublicKey(sk)
		if err != nil {
			return nil, fmt.Errorf("failed to derive public key: %w", err)
		}
		h.pk = pk

		slog.Info("query handler initialized", "pubkey", pk[:16]+"...")
	} else {
		slog.Warn("query handler has no private key configured, responses disabled")
	}

	return h, nil
}

// parsePrivateKey parses a private key from hex or nsec format.
func parsePrivateKey(key string) (string, error) {
	key = strings.TrimSpace(key)

	if strings.HasPrefix(key, "nsec1") {
		_, data, err := nip19.Decode(key)
		if err != nil {
			return "", fmt.Errorf("invalid nsec: %w", err)
		}
		return data.(string), nil
	}

	if len(key) != 64 {
		return "", fmt.Errorf("hex key must be 64 characters, got %d", len(key))
	}
	if _, err := hex.DecodeString(key); err != nil {
		return "", fmt.Errorf("invalid hex key: %w", err)
	}

	return key, nil
}

// Start begins listening for discovery queries.
func (h *Handler) Start(ctx context.Context) {
	if h.sk == "" {
		slog.Info("query handler not starting - no private key configured")
		return
	}

	slog.Info("query handler starting", "seed_relays", len(h.cfg.SeedRelays))

	// Subscribe to seed relays for kind 30068 events
	for _, relayURL := range h.cfg.SeedRelays {
		go h.subscribeToRelay(ctx, relayURL)
	}

	<-ctx.Done()
	slog.Info("query handler stopped")
}

// subscribeToRelay subscribes to a relay for query events.
func (h *Handler) subscribeToRelay(ctx context.Context, relayURL string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		h.listenForQueries(ctx, relayURL)

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// listenForQueries connects to a relay and listens for queries.
func (h *Handler) listenForQueries(ctx context.Context, relayURL string) {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		slog.Debug("failed to connect for queries", "url", relayURL, "error", err)
		return
	}
	defer relay.Close()

	// Subscribe to kind 30068 events
	since := nostr.Timestamp(time.Now().Add(-5 * time.Minute).Unix())
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds: []int{30068},
			Since: &since,
		},
	})
	if err != nil {
		slog.Debug("failed to subscribe for queries", "url", relayURL, "error", err)
		return
	}
	defer sub.Unsub()

	slog.Debug("subscribed for discovery queries", "relay", relayURL)

eventLoop:
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events:
			if !ok {
				break eventLoop
			}
			go h.handleQuery(ctx, event)
		}
	}
}

// handleQuery processes a single discovery query.
func (h *Handler) handleQuery(ctx context.Context, event *nostr.Event) {
	if event.Kind != 30068 {
		return
	}

	// Parse query parameters
	query := h.parseQuery(event)
	if query.Type == "" {
		slog.Debug("query missing type", "event_id", event.ID[:16])
		return
	}

	slog.Debug("handling discovery query",
		"type", query.Type,
		"from", event.PubKey[:16],
		"response_relay", query.ResponseRelay,
	)

	// Process based on query type
	var responses []*nostr.Event
	var err error

	switch query.Type {
	case "pubkey_location":
		responses, err = h.handlePubkeyLocation(ctx, query)
	case "find_relays":
		responses, err = h.handleFindRelays(ctx, query)
	case "active_streams":
		responses, err = h.handleActiveStreams(ctx, query)
	case "online_users":
		responses, err = h.handleOnlineUsers(ctx, query)
	default:
		slog.Debug("unknown query type", "type", query.Type)
		return
	}

	if err != nil {
		slog.Error("query handler error", "type", query.Type, "error", err)
		return
	}

	// Publish responses
	if len(responses) > 0 && query.ResponseRelay != "" {
		h.publishResponses(ctx, query.ResponseRelay, responses, event.ID)
	}

	h.mu.Lock()
	h.queriesHandled++
	h.lastQuery = time.Now()
	h.mu.Unlock()

	// Update stats
	h.cache.IncrementStat(ctx, "queries:handled")
	h.cache.IncrementStat(ctx, "queries:"+query.Type)
}

// Query represents a parsed discovery query.
type Query struct {
	Type          string
	ResponseRelay string
	Pubkeys       []string
	Health        string
	NIPs          []int
	Location      string
	Payment       string
	Admission     string
	Limit         int
	Topics        []string
}

// parseQuery extracts query parameters from event tags.
func (h *Handler) parseQuery(event *nostr.Event) Query {
	q := Query{
		Limit: 20, // default
	}

	for _, tag := range event.Tags {
		if len(tag) < 2 {
			continue
		}

		switch tag[0] {
		case "query_type":
			q.Type = tag[1]
		case "response_relay":
			q.ResponseRelay = tag[1]
		case "p":
			q.Pubkeys = append(q.Pubkeys, tag[1])
		case "health":
			q.Health = tag[1]
		case "nips":
			for _, nipStr := range tag[1:] {
				if nip, err := strconv.Atoi(nipStr); err == nil {
					q.NIPs = append(q.NIPs, nip)
				}
			}
		case "location":
			q.Location = tag[1]
		case "payment":
			q.Payment = tag[1]
		case "admission":
			q.Admission = tag[1]
		case "limit":
			if limit, err := strconv.Atoi(tag[1]); err == nil && limit > 0 {
				q.Limit = limit
				if q.Limit > 100 {
					q.Limit = 100 // cap at 100
				}
			}
		case "t":
			q.Topics = append(q.Topics, tag[1])
		}
	}

	return q
}

// handlePubkeyLocation finds relays that have content from specified pubkeys.
func (h *Handler) handlePubkeyLocation(ctx context.Context, query Query) ([]*nostr.Event, error) {
	if len(query.Pubkeys) == 0 {
		return nil, fmt.Errorf("no pubkeys specified")
	}

	var responses []*nostr.Event

	for _, pubkey := range query.Pubkeys {
		relays, err := h.cache.GetPubkeyRelays(ctx, pubkey)
		if err != nil {
			continue
		}

		// Create a response event with relay hints
		event := h.createPubkeyLocationResponse(pubkey, relays)
		responses = append(responses, event)
	}

	return responses, nil
}

// createPubkeyLocationResponse creates a response event for pubkey location query.
func (h *Handler) createPubkeyLocationResponse(pubkey string, relays []string) *nostr.Event {
	tags := nostr.Tags{
		{"p", pubkey},
		{"query_type", "pubkey_location"},
	}

	for _, relay := range relays {
		tags = append(tags, nostr.Tag{"relay", relay})
	}

	event := &nostr.Event{
		Kind:      30069, // Use relay directory entry kind for responses
		PubKey:    h.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   fmt.Sprintf("Relay locations for pubkey %s", pubkey[:16]),
	}

	event.Sign(h.sk)
	return event
}

// handleFindRelays finds relays matching specified criteria.
func (h *Handler) handleFindRelays(ctx context.Context, query Query) ([]*nostr.Event, error) {
	var candidateURLs []string

	// Start with all relays or filter by NIP/location
	if len(query.NIPs) > 0 {
		// Get relays supporting first NIP, then filter
		urls, err := h.cache.GetRelaysByNIP(ctx, query.NIPs[0])
		if err == nil {
			candidateURLs = urls
		}
	} else if query.Location != "" {
		urls, err := h.cache.GetRelaysByLocation(ctx, query.Location)
		if err == nil {
			candidateURLs = urls
		}
	} else {
		urls, err := h.cache.GetAllRelayURLs(ctx)
		if err == nil {
			candidateURLs = urls
		}
	}

	var responses []*nostr.Event
	count := 0

	for _, url := range candidateURLs {
		if count >= query.Limit {
			break
		}

		entry, err := h.cache.GetRelayEntry(ctx, url)
		if err != nil || entry == nil {
			continue
		}

		// Apply filters
		if query.Health != "" && entry.Health != query.Health {
			continue
		}
		if query.Payment != "" {
			wantPaid := query.Payment == "paid"
			if entry.PaymentRequired != wantPaid {
				continue
			}
		}
		if query.Admission != "" {
			if query.Admission == "open" && entry.AuthRequired {
				continue
			}
		}

		// Check NIP support
		if len(query.NIPs) > 1 {
			supported := make(map[int]bool)
			for _, nip := range entry.SupportedNIPs {
				supported[nip] = true
			}
			allSupported := true
			for _, nip := range query.NIPs {
				if !supported[nip] {
					allSupported = false
					break
				}
			}
			if !allSupported {
				continue
			}
		}

		// Create response event
		event := h.createRelayResponse(entry)
		responses = append(responses, event)
		count++
	}

	return responses, nil
}

// createRelayResponse creates a kind 30069 event from a relay entry.
func (h *Handler) createRelayResponse(entry *cache.RelayEntry) *nostr.Event {
	tags := nostr.Tags{
		{"d", entry.URL},
		{"relay", entry.URL},
		{"health", entry.Health},
		{"last_checked", strconv.FormatInt(entry.LastChecked.Unix(), 10)},
	}

	if entry.Name != "" {
		tags = append(tags, nostr.Tag{"name", entry.Name})
	}
	if entry.Description != "" {
		tags = append(tags, nostr.Tag{"description", entry.Description})
	}
	if len(entry.SupportedNIPs) > 0 {
		nipTag := nostr.Tag{"nips"}
		for _, nip := range entry.SupportedNIPs {
			nipTag = append(nipTag, strconv.Itoa(nip))
		}
		tags = append(tags, nipTag)
	}
	if entry.LatencyMs > 0 {
		tags = append(tags, nostr.Tag{"latency_ms", strconv.Itoa(entry.LatencyMs)})
	}
	if entry.CountryCode != "" {
		tags = append(tags, nostr.Tag{"location", entry.CountryCode})
	}
	if entry.PaymentRequired {
		tags = append(tags, nostr.Tag{"payment", "paid"})
	} else {
		tags = append(tags, nostr.Tag{"payment", "free"})
	}

	event := &nostr.Event{
		Kind:      30069,
		PubKey:    h.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	event.Sign(h.sk)
	return event
}

// handleActiveStreams returns current active streams.
func (h *Handler) handleActiveStreams(ctx context.Context, query Query) ([]*nostr.Event, error) {
	streamPubkeys, err := h.cache.GetActiveStreams(ctx)
	if err != nil {
		return nil, err
	}

	var responses []*nostr.Event
	count := 0

	for _, pubkey := range streamPubkeys {
		if count >= query.Limit {
			break
		}

		activity, err := h.cache.GetActivity(ctx, pubkey)
		if err != nil || activity == nil {
			continue
		}

		// Filter by topics if specified
		if len(query.Topics) > 0 {
			// TODO: implement topic filtering when we store topics
			continue
		}

		event := h.createActivityResponse(activity)
		responses = append(responses, event)
		count++
	}

	return responses, nil
}

// createActivityResponse creates a kind 30067 event from an activity.
func (h *Handler) createActivityResponse(activity *cache.Activity) *nostr.Event {
	tags := nostr.Tags{
		{"d", activity.Type},
		{"activity", activity.Type},
		{"p", activity.Pubkey},
		{"status", "active"},
	}

	if activity.URL != "" {
		tags = append(tags, nostr.Tag{"url", activity.URL})
	}
	if !activity.ExpiresAt.IsZero() {
		tags = append(tags, nostr.Tag{"expires", strconv.FormatInt(activity.ExpiresAt.Unix(), 10)})
	}

	event := &nostr.Event{
		Kind:      30067,
		PubKey:    h.pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   activity.Details,
	}

	event.Sign(h.sk)
	return event
}

// handleOnlineUsers returns currently online users.
func (h *Handler) handleOnlineUsers(ctx context.Context, query Query) ([]*nostr.Event, error) {
	// For now, we return users with any activity type
	// In the future, we could track "online" specifically

	// If specific pubkeys requested, check their status
	if len(query.Pubkeys) > 0 {
		var responses []*nostr.Event
		for _, pubkey := range query.Pubkeys {
			activity, err := h.cache.GetActivity(ctx, pubkey)
			if err != nil || activity == nil {
				continue
			}
			event := h.createActivityResponse(activity)
			responses = append(responses, event)
		}
		return responses, nil
	}

	// Otherwise return all active users (limited)
	return h.handleActiveStreams(ctx, query)
}

// publishResponses publishes response events to the specified relay.
func (h *Handler) publishResponses(ctx context.Context, relayURL string, responses []*nostr.Event, queryID string) {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		slog.Debug("failed to connect to response relay", "url", relayURL, "error", err)
		return
	}
	defer relay.Close()

	for _, event := range responses {
		// Add reference to original query
		event.Tags = append(event.Tags, nostr.Tag{"e", queryID, "", "reply"})
		event.Sign(h.sk) // Re-sign with updated tags

		if err := relay.Publish(ctx, *event); err != nil {
			slog.Debug("failed to publish response", "url", relayURL, "error", err)
			continue
		}
	}

	slog.Debug("published query responses",
		"relay", relayURL,
		"count", len(responses),
		"query_id", queryID[:16],
	)
}

// Stats returns query handler statistics.
func (h *Handler) GetQueriesHandled() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.queriesHandled
}

// GetLastQuery returns the time of the last query handled.
func (h *Handler) GetLastQuery() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastQuery
}

// GetPublicKey returns the handler's public key.
func (h *Handler) GetPublicKey() string {
	return h.pk
}
