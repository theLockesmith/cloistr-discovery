// Package query handles kind 30071 discovery query events.
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

// Handler processes kind 30071 discovery queries.
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

	// Subscribe to seed relays for kind 30071 events
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

	// Subscribe to kind 30071 events
	since := nostr.Timestamp(time.Now().Add(-5 * time.Minute).Unix())
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds: []int{30071},
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
	if event.Kind != 30071 {
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
	Atmospheres   []string
	ContentPolicy string
	Moderation    string
	Language      string
	Community     string
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
		case "atmosphere":
			q.Atmospheres = append(q.Atmospheres, tag[1])
		case "content_policy":
			q.ContentPolicy = tag[1]
		case "moderation":
			q.Moderation = tag[1]
		case "language":
			q.Language = tag[1]
		case "community":
			q.Community = tag[1]
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
		Kind:      30072, // Use relay directory entry kind for responses
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
	hasInitialFilter := false

	// Build candidate set using index lookups
	// NIP filter (AND logic - intersect results for each NIP)
	if len(query.NIPs) > 0 {
		hasInitialFilter = true
		for _, nip := range query.NIPs {
			urls, err := h.cache.GetRelaysByNIP(ctx, nip)
			if err != nil {
				continue
			}
			if len(candidateURLs) == 0 {
				candidateURLs = urls
			} else {
				candidateURLs = intersectStrings(candidateURLs, urls)
			}
		}
	}

	// Location filter
	if query.Location != "" {
		hasInitialFilter = true
		urls, err := h.cache.GetRelaysByLocation(ctx, query.Location)
		if err == nil {
			if len(candidateURLs) == 0 {
				candidateURLs = urls
			} else {
				candidateURLs = intersectStrings(candidateURLs, urls)
			}
		}
	}

	// Topic filter (OR logic - union of topics, then intersect with candidates)
	if len(query.Topics) > 0 {
		hasInitialFilter = true
		var topicURLs []string
		for _, topic := range query.Topics {
			urls, err := h.cache.GetRelaysByTopic(ctx, topic)
			if err == nil {
				topicURLs = unionStrings(topicURLs, urls)
			}
		}
		if len(candidateURLs) == 0 {
			candidateURLs = topicURLs
		} else {
			candidateURLs = intersectStrings(candidateURLs, topicURLs)
		}
	}

	// Atmosphere filter (OR logic - union of atmospheres, then intersect)
	if len(query.Atmospheres) > 0 {
		hasInitialFilter = true
		var atmURLs []string
		for _, atm := range query.Atmospheres {
			urls, err := h.cache.GetRelaysByAtmosphere(ctx, atm)
			if err == nil {
				atmURLs = unionStrings(atmURLs, urls)
			}
		}
		if len(candidateURLs) == 0 {
			candidateURLs = atmURLs
		} else {
			candidateURLs = intersectStrings(candidateURLs, atmURLs)
		}
	}

	// Content policy filter
	if query.ContentPolicy != "" {
		hasInitialFilter = true
		urls, err := h.cache.GetRelaysByContentPolicy(ctx, query.ContentPolicy)
		if err == nil {
			if len(candidateURLs) == 0 {
				candidateURLs = urls
			} else {
				candidateURLs = intersectStrings(candidateURLs, urls)
			}
		}
	}

	// Moderation filter (minimum level - includes requested level and above)
	if query.Moderation != "" {
		hasInitialFilter = true
		levels := moderationLevelsAtOrAbove(query.Moderation)
		var modURLs []string
		for _, level := range levels {
			urls, err := h.cache.GetRelaysByModeration(ctx, level)
			if err == nil {
				modURLs = unionStrings(modURLs, urls)
			}
		}
		if len(candidateURLs) == 0 {
			candidateURLs = modURLs
		} else {
			candidateURLs = intersectStrings(candidateURLs, modURLs)
		}
	}

	// Language filter
	if query.Language != "" {
		hasInitialFilter = true
		urls, err := h.cache.GetRelaysByLanguage(ctx, query.Language)
		if err == nil {
			if len(candidateURLs) == 0 {
				candidateURLs = urls
			} else {
				candidateURLs = intersectStrings(candidateURLs, urls)
			}
		}
	}

	// Community filter
	if query.Community != "" {
		hasInitialFilter = true
		urls, err := h.cache.GetRelaysByCommunity(ctx, query.Community)
		if err == nil {
			if len(candidateURLs) == 0 {
				candidateURLs = urls
			} else {
				candidateURLs = intersectStrings(candidateURLs, urls)
			}
		}
	}

	// If no index filters, start with all relays
	if !hasInitialFilter {
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

		// Apply post-fetch filters
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

		// Create response event
		event := h.createRelayResponse(entry)
		responses = append(responses, event)
		count++
	}

	return responses, nil
}

// intersectStrings returns the intersection of two string slices.
func intersectStrings(a, b []string) []string {
	m := make(map[string]bool)
	for _, v := range a {
		m[v] = true
	}
	var result []string
	for _, v := range b {
		if m[v] {
			result = append(result, v)
		}
	}
	return result
}

// unionStrings returns the union of two string slices (deduplicated).
func unionStrings(a, b []string) []string {
	m := make(map[string]bool)
	for _, v := range a {
		m[v] = true
	}
	for _, v := range b {
		m[v] = true
	}
	result := make([]string, 0, len(m))
	for v := range m {
		result = append(result, v)
	}
	return result
}

// moderationLevelsAtOrAbove returns all moderation levels at or above the given level.
// Ordering: unmoderated < light < active < strict
func moderationLevelsAtOrAbove(level string) []string {
	levels := []string{"unmoderated", "light", "active", "strict"}
	var result []string
	found := false
	for _, l := range levels {
		if l == level {
			found = true
		}
		if found {
			result = append(result, l)
		}
	}
	if !found {
		return []string{level}
	}
	return result
}

// createRelayResponse creates a kind 30072 event from a relay entry.
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
	if entry.Pubkey != "" {
		tags = append(tags, nostr.Tag{"operator", entry.Pubkey})
	}
	if entry.Software != "" {
		if entry.Version != "" {
			tags = append(tags, nostr.Tag{"software", entry.Software, entry.Version})
		} else {
			tags = append(tags, nostr.Tag{"software", entry.Software})
		}
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
	if entry.AuthRequired {
		tags = append(tags, nostr.Tag{"admission", "auth"})
	} else {
		tags = append(tags, nostr.Tag{"admission", "open"})
	}

	// Community & segregation metadata
	if entry.ContentPolicy != "" {
		tags = append(tags, nostr.Tag{"content_policy", entry.ContentPolicy})
	}
	if entry.Moderation != "" {
		tags = append(tags, nostr.Tag{"moderation", entry.Moderation})
	}
	if entry.ModerationPolicy != "" {
		tags = append(tags, nostr.Tag{"moderation_policy", entry.ModerationPolicy})
	}
	if entry.Community != "" {
		tags = append(tags, nostr.Tag{"community", entry.Community})
	}
	for _, lang := range entry.Languages {
		tags = append(tags, nostr.Tag{"language", lang})
	}

	// Aggregated annotation data
	for topic, count := range entry.Topics {
		tags = append(tags, nostr.Tag{"topic", topic, strconv.Itoa(count)})
	}
	for atm, count := range entry.Atmosphere {
		tags = append(tags, nostr.Tag{"atmosphere", atm, strconv.Itoa(count)})
	}

	event := &nostr.Event{
		Kind:      30072,
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
		Kind:      30070,
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
// Handles NIP-42 auth if the relay requires it.
func (h *Handler) publishResponses(ctx context.Context, relayURL string, responses []*nostr.Event, queryID string) {
	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		slog.Debug("failed to connect to response relay", "url", relayURL, "error", err)
		return
	}
	defer relay.Close()

	authAttempted := false
	published := 0

	for _, event := range responses {
		// Add reference to original query
		event.Tags = append(event.Tags, nostr.Tag{"e", queryID, "", "reply"})
		event.Sign(h.sk) // Re-sign with updated tags

		err := relay.Publish(ctx, *event)
		if err == nil {
			published++
			continue
		}

		// Check if auth is required
		errStr := err.Error()
		if strings.Contains(errStr, "auth-required") && !authAttempted {
			authAttempted = true
			slog.Debug("response relay requires auth, authenticating", "url", relayURL)

			authErr := relay.Auth(ctx, func(authEvent *nostr.Event) error {
				return authEvent.Sign(h.sk)
			})
			if authErr != nil {
				slog.Warn("NIP-42 auth failed for response relay", "url", relayURL, "error", authErr)
				return
			}
			slog.Info("NIP-42 auth successful for response relay", "url", relayURL)

			// Retry this event after auth
			if retryErr := relay.Publish(ctx, *event); retryErr != nil {
				slog.Debug("publish response still failed after auth", "url", relayURL, "error", retryErr)
			} else {
				published++
			}
			continue
		}

		slog.Debug("failed to publish response", "url", relayURL, "error", err)
	}

	if published > 0 {
		slog.Debug("published query responses",
			"relay", relayURL,
			"count", published,
			"total", len(responses),
			"query_id", queryID[:16],
		)
	}
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
