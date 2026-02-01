// Package activity handles activity announcement tracking.
// Implements Kind 30067 (Activity Announcement) from NDP.
// Tracks real-time user activities like streaming, online status, etc.
package activity

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"gitlab.com/coldforge/coldforge-discovery/internal/cache"
	"gitlab.com/coldforge/coldforge-discovery/internal/config"
)

// Known activity types from the NDP registry
const (
	ActivityStreaming = "streaming" // Live audio/video broadcast
	ActivityOnline    = "online"    // General online presence
	ActivityWriting   = "writing"   // Writing/creating content
	ActivityCoding    = "coding"    // Coding/development
	ActivityGaming    = "gaming"    // Playing games
	ActivityListening = "listening" // Listening to music/podcast
	ActivityWatching  = "watching"  // Watching video content
)

// Tracker monitors activity announcements and maintains the activity index.
type Tracker struct {
	cfg   *config.Config
	cache *cache.Client

	mu            sync.RWMutex
	subscriptions map[string]context.CancelFunc
}

// NewTracker creates a new activity tracker.
func NewTracker(cfg *config.Config, cache *cache.Client) *Tracker {
	return &Tracker{
		cfg:           cfg,
		cache:         cache,
		subscriptions: make(map[string]context.CancelFunc),
	}
}

// Start begins activity tracking.
func (t *Tracker) Start(ctx context.Context) {
	slog.Info("activity tracker started")

	// Start cleanup goroutine for expired activities
	go t.cleanupLoop(ctx)

	<-ctx.Done()
	t.stopAllSubscriptions()
	slog.Info("activity tracker stopped")
}

// ProcessActivity processes a Kind 30067 activity announcement event.
func (t *Tracker) ProcessActivity(ctx context.Context, event *nostr.Event) error {
	if event.Kind != 30067 {
		return nil
	}

	// Extract activity type from 'd' tag
	var activityType string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			activityType = tag[1]
			break
		}
	}
	if activityType == "" {
		slog.Warn("activity event missing type", "id", event.ID)
		return nil
	}

	// Extract optional details
	var details, url string
	for _, tag := range event.Tags {
		if len(tag) >= 2 {
			switch tag[0] {
			case "details":
				details = tag[1]
			case "url":
				url = tag[1]
			}
		}
	}

	// Calculate expiration
	ttl := time.Duration(t.cfg.ActivityTTL) * time.Minute
	expiresAt := time.Now().Add(ttl)

	// Check for explicit expiration tag
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "expiration" {
			// Parse expiration timestamp
			// TODO: implement expiration parsing
		}
	}

	activity := &cache.Activity{
		Pubkey:    event.PubKey,
		Type:      activityType,
		Details:   details,
		URL:       url,
		CreatedAt: event.CreatedAt.Time(),
		ExpiresAt: expiresAt,
	}

	if err := t.cache.SetActivity(ctx, activity, ttl); err != nil {
		slog.Error("failed to cache activity", "pubkey", event.PubKey, "type", activityType, "error", err)
		return err
	}

	slog.Debug("processed activity", "pubkey", event.PubKey, "type", activityType)
	return nil
}

// ClearActivity processes activity end events (empty content or delete).
func (t *Tracker) ClearActivity(ctx context.Context, pubkey string) error {
	if err := t.cache.ClearActivity(ctx, pubkey); err != nil {
		slog.Error("failed to clear activity", "pubkey", pubkey, "error", err)
		return err
	}
	slog.Debug("cleared activity", "pubkey", pubkey)
	return nil
}

// SubscribeToRelay subscribes to a relay for activity updates.
func (t *Tracker) SubscribeToRelay(ctx context.Context, relayURL string) error {
	t.mu.Lock()
	if _, exists := t.subscriptions[relayURL]; exists {
		t.mu.Unlock()
		return nil
	}

	subCtx, cancel := context.WithCancel(ctx)
	t.subscriptions[relayURL] = cancel
	t.mu.Unlock()

	go func() {
		t.subscribeLoop(subCtx, relayURL)
	}()

	return nil
}

func (t *Tracker) subscribeLoop(ctx context.Context, relayURL string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		relay, err := nostr.RelayConnect(ctx, relayURL)
		if err != nil {
			slog.Error("failed to connect to relay", "url", relayURL, "error", err)
			time.Sleep(30 * time.Second)
			continue
		}

		// Subscribe to Kind 30067 events
		sub, err := relay.Subscribe(ctx, []nostr.Filter{
			{Kinds: []int{30067}},
		})
		if err != nil {
			slog.Error("failed to subscribe", "url", relayURL, "error", err)
			relay.Close()
			time.Sleep(30 * time.Second)
			continue
		}

	eventLoop:
		for {
			select {
			case <-ctx.Done():
				sub.Unsub()
				relay.Close()
				return
			case event, ok := <-sub.Events:
				if !ok {
					break eventLoop
				}
				// Empty content means activity ended
				if event.Content == "" || event.Content == "offline" {
					t.ClearActivity(ctx, event.PubKey)
				} else {
					t.ProcessActivity(ctx, event)
				}
			}
		}

		relay.Close()
		time.Sleep(5 * time.Second)
	}
}

func (t *Tracker) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Activities expire via Redis TTL, but we could do additional cleanup here
			slog.Debug("activity cleanup tick")
		}
	}
}

func (t *Tracker) stopAllSubscriptions() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, cancel := range t.subscriptions {
		cancel()
	}
	t.subscriptions = make(map[string]context.CancelFunc)
}

// UnsubscribeFromRelay stops the subscription to a relay.
func (t *Tracker) UnsubscribeFromRelay(relayURL string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cancel, exists := t.subscriptions[relayURL]; exists {
		cancel()
		delete(t.subscriptions, relayURL)
	}
}
