package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"

	"git.aegis-hq.xyz/coldforge/cloistr-discovery/internal/cache"
)

func TestRelayReviewsHandler(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()

	// Setup cached reviews for a relay
	reviews := &cache.RelayReviewsEntry{
		RelayURL: "wss://reviewed.relay.com",
		Reviews: []cache.RelayReview{
			{
				Pubkey:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Rating:    5,
				Comment:   "Excellent relay!",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
			{
				Pubkey:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Rating:    3,
				Comment:   "Average performance",
				CreatedAt: time.Now().Add(-2 * time.Hour),
			},
		},
		AverageRating: 4.0,
		TotalReviews:  2,
		FetchedAt:     time.Now(),
	}
	server.cache.SetRelayReviews(ctx, reviews)

	tests := []struct {
		name           string
		method         string
		queryParams    string
		wantStatusCode int
		wantError      string
		checkResponse  func(t *testing.T, resp RelayReviewsResponse)
	}{
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			queryParams:    "?url=wss://reviewed.relay.com",
			wantStatusCode: http.StatusMethodNotAllowed,
		},
		{
			name:           "missing url parameter",
			method:         http.MethodGet,
			queryParams:    "",
			wantStatusCode: http.StatusBadRequest,
			wantError:      "url parameter required",
		},
		{
			name:           "invalid url format",
			method:         http.MethodGet,
			queryParams:    "?url=https://invalid.com",
			wantStatusCode: http.StatusBadRequest,
			wantError:      "invalid relay URL format",
		},
		{
			name:           "returns cached reviews",
			method:         http.MethodGet,
			queryParams:    "?url=wss://reviewed.relay.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp RelayReviewsResponse) {
				if !resp.CacheHit {
					t.Error("expected cache hit")
				}
				if resp.TotalReviews != 2 {
					t.Errorf("TotalReviews = %d, want 2", resp.TotalReviews)
				}
				if resp.AverageRating != 4.0 {
					t.Errorf("AverageRating = %f, want 4.0", resp.AverageRating)
				}
				if len(resp.Reviews) != 2 {
					t.Errorf("Reviews count = %d, want 2", len(resp.Reviews))
				}
			},
		},
		{
			name:           "uncached relay returns empty",
			method:         http.MethodGet,
			queryParams:    "?url=wss://unreviewed.relay.com",
			wantStatusCode: http.StatusOK,
			checkResponse: func(t *testing.T, resp RelayReviewsResponse) {
				if resp.CacheHit {
					t.Error("expected cache miss")
				}
				if resp.TotalReviews != 0 {
					t.Errorf("TotalReviews = %d, want 0", resp.TotalReviews)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/relay/reviews"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			server.RelayReviewsHandler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatusCode {
				t.Errorf("status = %v, want %v", resp.StatusCode, tt.wantStatusCode)
				return
			}

			if tt.wantStatusCode == http.StatusMethodNotAllowed {
				return
			}

			if tt.wantError != "" {
				var errResp map[string]string
				json.NewDecoder(resp.Body).Decode(&errResp)
				if errResp["error"] != tt.wantError {
					t.Errorf("error = %q, want %q", errResp["error"], tt.wantError)
				}
				return
			}

			if tt.checkResponse != nil {
				var reviewsResp RelayReviewsResponse
				if err := json.NewDecoder(resp.Body).Decode(&reviewsResp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				tt.checkResponse(t, reviewsResp)
			}
		})
	}
}

func TestRelayReviewsWithWoT(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	userPubkey := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	followPubkey := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	strangerPubkey := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	// Setup user's contacts
	contacts := &cache.UserContactsEntry{
		Pubkey:    userPubkey,
		Follows:   []string{followPubkey},
		FetchedAt: time.Now(),
	}
	server.cache.SetUserContacts(ctx, contacts)

	// Setup reviews - one from follow, one from stranger
	reviews := &cache.RelayReviewsEntry{
		RelayURL: "wss://wot-test.relay.com",
		Reviews: []cache.RelayReview{
			{
				Pubkey:    followPubkey,
				Rating:    5,
				Comment:   "Review from someone I follow",
				CreatedAt: time.Now(),
			},
			{
				Pubkey:    strangerPubkey,
				Rating:    2,
				Comment:   "Review from stranger",
				CreatedAt: time.Now(),
			},
		},
		AverageRating: 3.5,
		TotalReviews:  2,
		FetchedAt:     time.Now(),
	}
	server.cache.SetRelayReviews(ctx, reviews)

	// Request with pubkey for WoT weighting
	req := httptest.NewRequest(http.MethodGet, "/api/v1/relay/reviews?url=wss://wot-test.relay.com&pubkey="+userPubkey, nil)
	w := httptest.NewRecorder()

	server.RelayReviewsHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %v, want 200", resp.StatusCode)
	}

	var reviewsResp RelayReviewsResponse
	if err := json.NewDecoder(resp.Body).Decode(&reviewsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check WoT metrics
	if reviewsResp.WoTReviewCount != 1 {
		t.Errorf("WoTReviewCount = %d, want 1", reviewsResp.WoTReviewCount)
	}
	if reviewsResp.WoTAverage != 5.0 {
		t.Errorf("WoTAverage = %f, want 5.0", reviewsResp.WoTAverage)
	}

	// Check InNetwork flag
	for _, review := range reviewsResp.Reviews {
		if review.Pubkey == followPubkey && !review.InNetwork {
			t.Error("follow's review should have InNetwork=true")
		}
		if review.Pubkey == strangerPubkey && review.InNetwork {
			t.Error("stranger's review should have InNetwork=false")
		}
	}
}

func TestParseReviewEvent(t *testing.T) {
	tests := []struct {
		name    string
		event   *nostr.Event
		want    *RelayReviewEntry
		wantNil bool
	}{
		{
			name:    "nil event",
			event:   nil,
			wantNil: true,
		},
		{
			name: "valid review",
			event: &nostr.Event{
				PubKey:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Content:   `{"rating": 4, "comment": "Good relay"}`,
				CreatedAt: nostr.Timestamp(time.Now().Unix()),
			},
			want: &RelayReviewEntry{
				Pubkey:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Rating:  4,
				Comment: "Good relay",
			},
		},
		{
			name: "rating only",
			event: &nostr.Event{
				PubKey:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Content:   `{"rating": 5}`,
				CreatedAt: nostr.Timestamp(time.Now().Unix()),
			},
			want: &RelayReviewEntry{
				Pubkey:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Rating:  5,
				Comment: "",
			},
		},
		{
			name: "invalid json",
			event: &nostr.Event{
				PubKey:    "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				Content:   "not json",
				CreatedAt: nostr.Timestamp(time.Now().Unix()),
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReviewEvent(tt.event)

			if tt.wantNil {
				if got != nil {
					t.Errorf("parseReviewEvent() = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("parseReviewEvent() = nil, want non-nil")
			}

			if got.Pubkey != tt.want.Pubkey {
				t.Errorf("Pubkey = %s, want %s", got.Pubkey, tt.want.Pubkey)
			}
			if got.Rating != tt.want.Rating {
				t.Errorf("Rating = %d, want %d", got.Rating, tt.want.Rating)
			}
			if got.Comment != tt.want.Comment {
				t.Errorf("Comment = %s, want %s", got.Comment, tt.want.Comment)
			}
		})
	}
}

func TestRelayReviewsCache(t *testing.T) {
	server, mr := setupTestServer(t)
	defer mr.Close()

	ctx := context.Background()
	relayURL := "wss://cache-test.relay.com"

	// Initially should be empty
	cached, err := server.cache.GetRelayReviews(ctx, relayURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached != nil {
		t.Error("expected nil for uncached reviews")
	}

	// Set reviews
	entry := &cache.RelayReviewsEntry{
		RelayURL: relayURL,
		Reviews: []cache.RelayReview{
			{Pubkey: "aaa", Rating: 5, Comment: "Great!", CreatedAt: time.Now()},
		},
		AverageRating: 5.0,
		TotalReviews:  1,
		FetchedAt:     time.Now(),
	}
	if err := server.cache.SetRelayReviews(ctx, entry); err != nil {
		t.Fatalf("failed to set reviews: %v", err)
	}

	// Should be cached now
	cached, err = server.cache.GetRelayReviews(ctx, relayURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached reviews")
	}
	if cached.TotalReviews != 1 {
		t.Errorf("TotalReviews = %d, want 1", cached.TotalReviews)
	}
	if cached.AverageRating != 5.0 {
		t.Errorf("AverageRating = %f, want 5.0", cached.AverageRating)
	}
}
