// Package annotation handles Kind 30073 Relay Annotation events.
// Users publish annotations to tag relays with topics, atmosphere, and notes.
// Discovery services aggregate these to build searchable relay indexes.
package annotation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/redis/go-redis/v9"

	"gitlab.com/coldforge/coldforge-discovery/internal/config"
)

// Aggregator subscribes to relay annotations and aggregates topic/atmosphere data.
type Aggregator struct {
	cfg *config.Config
	rdb *redis.Client

	mu            sync.RWMutex
	subscriptions map[string]context.CancelFunc
}

// NewAggregator creates a new annotation aggregator.
func NewAggregator(cfg *config.Config, cacheURL string) (*Aggregator, error) {
	opts, err := redis.ParseURL(cacheURL)
	if err != nil {
		return nil, fmt.Errorf("invalid cache URL: %w", err)
	}

	return &Aggregator{
		cfg:           cfg,
		rdb:           redis.NewClient(opts),
		subscriptions: make(map[string]context.CancelFunc),
	}, nil
}

// Start begins annotation aggregation.
func (a *Aggregator) Start(ctx context.Context) {
	slog.Info("annotation aggregator started")
	<-ctx.Done()
	a.stopAllSubscriptions()
	slog.Info("annotation aggregator stopped")
}

// ProcessAnnotation processes a Kind 30073 relay annotation event.
func (a *Aggregator) ProcessAnnotation(ctx context.Context, event *nostr.Event) error {
	if event.Kind != 30073 {
		return nil
	}

	// Extract relay URL from 'd' tag
	var relayURL string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			relayURL = tag[1]
			break
		}
	}
	if relayURL == "" {
		slog.Debug("annotation event missing relay URL", "id", event.ID)
		return nil
	}

	// Extract topics and atmosphere from tags
	var topics []string
	var atmospheres []string
	for _, tag := range event.Tags {
		if len(tag) >= 2 {
			switch tag[0] {
			case "topic":
				topics = append(topics, tag[1])
			case "atmosphere":
				atmospheres = append(atmospheres, tag[1])
			}
		}
	}

	ttl := 6 * time.Hour

	// Store raw annotation reference (one per user per relay via 'd' tag)
	annotationKey := fmt.Sprintf("relay:annotations:%s", relayURL)
	a.rdb.SAdd(ctx, annotationKey, event.ID)
	a.rdb.Expire(ctx, annotationKey, ttl)

	// Increment topic counts for this relay
	topicKey := fmt.Sprintf("relay:topics:%s", relayURL)
	for _, topic := range topics {
		a.rdb.HIncrBy(ctx, topicKey, topic, 1)
	}
	a.rdb.Expire(ctx, topicKey, ttl)

	// Increment atmosphere counts for this relay
	atmKey := fmt.Sprintf("relay:atmosphere:%s", relayURL)
	for _, atm := range atmospheres {
		a.rdb.HIncrBy(ctx, atmKey, atm, 1)
	}
	a.rdb.Expire(ctx, atmKey, ttl)

	slog.Debug("processed annotation",
		"relay", relayURL,
		"topics", topics,
		"atmospheres", atmospheres,
		"author", event.PubKey[:16],
	)

	return nil
}

// GetRelayTopics returns aggregated topic counts for a relay.
func (a *Aggregator) GetRelayTopics(ctx context.Context, relayURL string) (map[string]int, error) {
	key := fmt.Sprintf("relay:topics:%s", relayURL)
	result, err := a.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	topics := make(map[string]int)
	for topic, countStr := range result {
		var count int
		fmt.Sscanf(countStr, "%d", &count)
		topics[topic] = count
	}
	return topics, nil
}

// GetRelayAtmosphere returns aggregated atmosphere counts for a relay.
func (a *Aggregator) GetRelayAtmosphere(ctx context.Context, relayURL string) (map[string]int, error) {
	key := fmt.Sprintf("relay:atmosphere:%s", relayURL)
	result, err := a.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	atmosphere := make(map[string]int)
	for atm, countStr := range result {
		var count int
		fmt.Sscanf(countStr, "%d", &count)
		atmosphere[atm] = count
	}
	return atmosphere, nil
}

// SubscribeToRelay subscribes to a relay for annotation events.
func (a *Aggregator) SubscribeToRelay(ctx context.Context, relayURL string) error {
	a.mu.Lock()
	if _, exists := a.subscriptions[relayURL]; exists {
		a.mu.Unlock()
		return nil
	}

	subCtx, cancel := context.WithCancel(ctx)
	a.subscriptions[relayURL] = cancel
	a.mu.Unlock()

	go a.subscribeLoop(subCtx, relayURL)
	return nil
}

func (a *Aggregator) subscribeLoop(ctx context.Context, relayURL string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		relay, err := nostr.RelayConnect(ctx, relayURL)
		if err != nil {
			slog.Debug("failed to connect for annotations", "url", relayURL, "error", err)
			time.Sleep(30 * time.Second)
			continue
		}

		// Subscribe to Kind 30073 events
		sub, err := relay.Subscribe(ctx, []nostr.Filter{
			{Kinds: []int{30073}},
		})
		if err != nil {
			slog.Debug("failed to subscribe for annotations", "url", relayURL, "error", err)
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
				if err := a.ProcessAnnotation(ctx, event); err != nil {
					slog.Error("failed to process annotation", "error", err)
				}
			}
		}

		relay.Close()
		time.Sleep(5 * time.Second)
	}
}

func (a *Aggregator) stopAllSubscriptions() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, cancel := range a.subscriptions {
		cancel()
	}
	a.subscriptions = make(map[string]context.CancelFunc)
}

// Close closes the aggregator's Redis connection.
func (a *Aggregator) Close() error {
	return a.rdb.Close()
}
