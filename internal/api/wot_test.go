package api

import (
	"context"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

func TestParseContactTags(t *testing.T) {
	tests := []struct {
		name string
		tags [][]string
		want []string
	}{
		{
			name: "valid contacts",
			tags: [][]string{
				{"p", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "wss://relay.example.com", "alice"},
				{"p", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "", ""},
			},
			want: []string{
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
		{
			name: "filters invalid pubkeys",
			tags: [][]string{
				{"p", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				{"p", "tooshort"},
				{"p", ""},
			},
			want: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		{
			name: "deduplicates",
			tags: [][]string{
				{"p", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				{"p", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			},
			want: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		{
			name: "ignores non-p tags",
			tags: [][]string{
				{"e", "someeventid"},
				{"p", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				{"r", "wss://relay.com"},
			},
			want: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		{
			name: "empty tags",
			tags: [][]string{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to nostr.Tags format
			tags := make(nostr.Tags, len(tt.tags))
			for i, tag := range tt.tags {
				tags[i] = nostr.Tag(tag)
			}

			got := parseContactTags(tags)

			if len(got) != len(tt.want) {
				t.Errorf("parseContactTags() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseContactTags()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWoTRelayScoresCache(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	pubkey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	// Initially should be empty
	cached, err := server.cache.GetWoTRelayScores(ctx, pubkey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached != nil {
		t.Error("expected nil for uncached WoT scores")
	}

	// Set scores
	entry := &cache.WoTRelayScoresEntry{
		Pubkey: pubkey,
		RelayScores: map[string]int{
			"wss://relay1.com": 5,
			"wss://relay2.com": 3,
		},
		FollowsCount: 10,
		ComputedAt:   time.Now(),
	}
	if err := server.cache.SetWoTRelayScores(ctx, entry); err != nil {
		t.Fatalf("failed to set WoT scores: %v", err)
	}

	// Should be cached now
	cached, err = server.cache.GetWoTRelayScores(ctx, pubkey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached WoT scores")
	}
	if cached.FollowsCount != 10 {
		t.Errorf("FollowsCount = %d, want 10", cached.FollowsCount)
	}
	if cached.RelayScores["wss://relay1.com"] != 5 {
		t.Errorf("relay1 score = %d, want 5", cached.RelayScores["wss://relay1.com"])
	}
}

func TestUserContactsCache(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	pubkey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	// Initially should be empty
	cached, err := server.cache.GetUserContacts(ctx, pubkey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached != nil {
		t.Error("expected nil for uncached contacts")
	}

	// Set contacts
	entry := &cache.UserContactsEntry{
		Pubkey: pubkey,
		Follows: []string{
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		},
		FetchedAt: time.Now(),
	}
	if err := server.cache.SetUserContacts(ctx, entry); err != nil {
		t.Fatalf("failed to set contacts: %v", err)
	}

	// Should be cached now
	cached, err = server.cache.GetUserContacts(ctx, pubkey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached contacts")
	}
	if len(cached.Follows) != 2 {
		t.Errorf("Follows count = %d, want 2", len(cached.Follows))
	}
}

func TestRecommendWithWoT(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	userPubkey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	// Setup relays
	relay1 := &cache.RelayEntry{
		URL:           "wss://popular.relay.com",
		Name:          "Popular Relay",
		Health:        "online",
		LatencyMs:     50,
		SupportedNIPs: []int{1, 11},
		LastChecked:   time.Now(),
	}
	relay2 := &cache.RelayEntry{
		URL:           "wss://unpopular.relay.com",
		Name:          "Unpopular Relay",
		Health:        "online",
		LatencyMs:     50,
		SupportedNIPs: []int{1, 11},
		LastChecked:   time.Now(),
	}

	server.cache.SetRelayEntry(ctx, relay1, time.Hour)
	server.cache.SetRelayEntry(ctx, relay2, time.Hour)

	// Setup WoT scores - popular relay is used by 5 follows
	wotEntry := &cache.WoTRelayScoresEntry{
		Pubkey: userPubkey,
		RelayScores: map[string]int{
			"wss://popular.relay.com": 5,
		},
		FollowsCount: 10,
		ComputedAt:   time.Now(),
	}
	server.cache.SetWoTRelayScores(ctx, wotEntry)

	// Get WoT scores
	scores, err := server.GetWoTRelayScores(ctx, userPubkey)
	if err != nil {
		t.Fatalf("failed to get WoT scores: %v", err)
	}

	if scores.RelayScores["wss://popular.relay.com"] != 5 {
		t.Errorf("popular relay score = %d, want 5", scores.RelayScores["wss://popular.relay.com"])
	}

	// The popular relay should have network_presence set when scoring
	// Since both relays are identical except WoT, popular should score higher
}
