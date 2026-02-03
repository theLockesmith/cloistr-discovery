// Command testclient is a CLI tool for testing the NDP discovery service.
// It can publish test events, send queries, and verify responses.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

func main() {
	// Commands
	cmd := flag.String("cmd", "help", "Command: help, keygen, publish-inventory, publish-activity, query, query-http, listen")

	// Connection settings
	relayURL := flag.String("relay", "ws://localhost:17080", "Relay WebSocket URL")
	apiURL := flag.String("api", "http://localhost:18080", "Discovery service HTTP API URL")

	// Key settings
	privateKey := flag.String("sk", "", "Private key (hex or nsec)")

	// Event settings
	targetPubkey := flag.String("pubkey", "", "Target pubkey for queries")
	queryType := flag.String("query-type", "find_relays", "Query type: pubkey_location, find_relays, active_streams, online_users")
	health := flag.String("health", "", "Health filter for find_relays")
	nips := flag.String("nips", "", "NIP filter (comma-separated)")
	activityType := flag.String("activity", "online", "Activity type for publish-activity")
	inventoryRelay := flag.String("inventory-relay", "", "Relay URL for inventory event")
	responseRelay := flag.String("response-relay", "", "Response relay for queries (default: same as -relay)")

	// Timeout
	timeout := flag.Duration("timeout", 30*time.Second, "Timeout for operations")

	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch *cmd {
	case "help":
		printHelp()

	case "keygen":
		generateKeypair()

	case "publish-inventory":
		sk := parsePrivateKey(*privateKey)
		if sk == "" {
			fmt.Println("Error: -sk (private key) required")
			os.Exit(1)
		}
		if *inventoryRelay == "" {
			fmt.Println("Error: -inventory-relay required")
			os.Exit(1)
		}
		publishInventory(ctx, *relayURL, sk, *inventoryRelay)

	case "publish-activity":
		sk := parsePrivateKey(*privateKey)
		if sk == "" {
			fmt.Println("Error: -sk (private key) required")
			os.Exit(1)
		}
		publishActivity(ctx, *relayURL, sk, *activityType)

	case "query":
		sk := parsePrivateKey(*privateKey)
		if sk == "" {
			fmt.Println("Error: -sk (private key) required")
			os.Exit(1)
		}
		respRelay := *responseRelay
		if respRelay == "" {
			respRelay = *relayURL
		}
		publishQuery(ctx, *relayURL, sk, *queryType, *targetPubkey, *health, *nips, respRelay)

	case "query-http":
		queryHTTP(ctx, *apiURL, *targetPubkey, *health)

	case "listen":
		listenForEvents(ctx, *relayURL)

	default:
		fmt.Printf("Unknown command: %s\n", *cmd)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println(`NDP Test Client - Test the Nostr Discovery Protocol implementation

Commands:
  keygen              Generate a new keypair for testing
  publish-inventory   Publish a kind 30066 relay inventory event
  publish-activity    Publish a kind 30067 activity announcement
  query               Publish a kind 30068 discovery query
  query-http          Query the discovery service via HTTP API
  listen              Listen for NDP events on a relay

Examples:
  # Generate a test keypair
  ./testclient -cmd keygen

  # Publish a test inventory event
  ./testclient -cmd publish-inventory -sk <privkey> -relay ws://localhost:17080 -inventory-relay wss://test.relay

  # Publish an activity announcement
  ./testclient -cmd publish-activity -sk <privkey> -relay ws://localhost:17080 -activity streaming

  # Send a discovery query
  ./testclient -cmd query -sk <privkey> -relay ws://localhost:17080 -query-type find_relays -health online

  # Query via HTTP API
  ./testclient -cmd query-http -api http://localhost:18080

  # Listen for NDP events
  ./testclient -cmd listen -relay ws://localhost:17080

Environment:
  NOSTR_PRIVATE_KEY   Default private key (can be overridden with -sk)
`)
}

func generateKeypair() {
	sk := nostr.GeneratePrivateKey()
	pk, _ := nostr.GetPublicKey(sk)
	nsec, _ := nip19.EncodePrivateKey(sk)
	npub, _ := nip19.EncodePublicKey(pk)

	fmt.Println("Generated Keypair:")
	fmt.Println("==================")
	fmt.Printf("Private Key (hex):  %s\n", sk)
	fmt.Printf("Private Key (nsec): %s\n", nsec)
	fmt.Printf("Public Key (hex):   %s\n", pk)
	fmt.Printf("Public Key (npub):  %s\n", npub)
	fmt.Println()
	fmt.Println("Export for shell:")
	fmt.Printf("export NOSTR_PRIVATE_KEY=%s\n", sk)
}

func parsePrivateKey(key string) string {
	if key == "" {
		key = os.Getenv("NOSTR_PRIVATE_KEY")
	}
	if key == "" {
		return ""
	}

	key = strings.TrimSpace(key)

	// Handle nsec format
	if strings.HasPrefix(key, "nsec1") {
		_, data, err := nip19.Decode(key)
		if err != nil {
			fmt.Printf("Error decoding nsec: %v\n", err)
			os.Exit(1)
		}
		return data.(string)
	}

	// Validate hex format
	if len(key) != 64 {
		fmt.Printf("Error: hex key must be 64 characters, got %d\n", len(key))
		os.Exit(1)
	}
	if _, err := hex.DecodeString(key); err != nil {
		fmt.Printf("Error: invalid hex key: %v\n", err)
		os.Exit(1)
	}

	return key
}

func publishInventory(ctx context.Context, relayURL, sk, inventoryRelayURL string) {
	pk, _ := nostr.GetPublicKey(sk)

	fmt.Printf("Publishing kind 30066 (Relay Inventory) to %s\n", relayURL)
	fmt.Printf("Inventory for relay: %s\n", inventoryRelayURL)

	// Create a test inventory event
	event := &nostr.Event{
		Kind:      30066,
		PubKey:    pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"d", inventoryRelayURL},
			{"relay", inventoryRelayURL},
			{"inventory_type", "pubkeys"},
			{"count", "3"},
			{"p", pk, "10", fmt.Sprintf("%d", time.Now().Unix())}, // Self
			{"p", "82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2", "5", fmt.Sprintf("%d", time.Now().Unix())},
			{"p", "3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d", "25", fmt.Sprintf("%d", time.Now().Unix())},
			{"expires", fmt.Sprintf("%d", time.Now().Add(6*time.Hour).Unix())},
		},
		Content: "",
	}

	event.Sign(sk)

	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		fmt.Printf("Error connecting to relay: %v\n", err)
		os.Exit(1)
	}
	defer relay.Close()

	err = relay.Publish(ctx, *event)
	if err != nil {
		fmt.Printf("Error publishing: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Published successfully!")
	fmt.Printf("Event ID: %s\n", event.ID)
	printEventJSON(event)
}

func publishActivity(ctx context.Context, relayURL, sk, activityType string) {
	pk, _ := nostr.GetPublicKey(sk)

	fmt.Printf("Publishing kind 30067 (Activity Announcement) to %s\n", relayURL)
	fmt.Printf("Activity type: %s\n", activityType)

	event := &nostr.Event{
		Kind:      30067,
		PubKey:    pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"d", activityType},
			{"activity", activityType},
			{"status", "active"},
			{"expires", fmt.Sprintf("%d", time.Now().Add(1*time.Hour).Unix())},
		},
		Content: fmt.Sprintf("Test %s activity from NDP test client", activityType),
	}

	if activityType == "streaming" {
		event.Tags = append(event.Tags,
			nostr.Tag{"title", "NDP Test Stream"},
			nostr.Tag{"summary", "Testing the Nostr Discovery Protocol"},
		)
	}

	event.Sign(sk)

	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		fmt.Printf("Error connecting to relay: %v\n", err)
		os.Exit(1)
	}
	defer relay.Close()

	err = relay.Publish(ctx, *event)
	if err != nil {
		fmt.Printf("Error publishing: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Published successfully!")
	fmt.Printf("Event ID: %s\n", event.ID)
	printEventJSON(event)
}

func publishQuery(ctx context.Context, relayURL, sk, queryType, targetPubkey, health, nips, responseRelayURL string) {
	pk, _ := nostr.GetPublicKey(sk)

	fmt.Printf("Publishing kind 30068 (Discovery Query) to %s\n", relayURL)
	fmt.Printf("Query type: %s\n", queryType)

	tags := nostr.Tags{
		{"query_type", queryType},
		{"response_relay", responseRelayURL},
		{"limit", "10"},
	}

	if targetPubkey != "" {
		tags = append(tags, nostr.Tag{"p", targetPubkey})
	}
	if health != "" {
		tags = append(tags, nostr.Tag{"health", health})
	}
	if nips != "" {
		nipTag := nostr.Tag{"nips"}
		for _, nip := range strings.Split(nips, ",") {
			nipTag = append(nipTag, strings.TrimSpace(nip))
		}
		tags = append(tags, nipTag)
	}

	event := &nostr.Event{
		Kind:      30068,
		PubKey:    pk,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	event.Sign(sk)

	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		fmt.Printf("Error connecting to relay: %v\n", err)
		os.Exit(1)
	}
	defer relay.Close()

	err = relay.Publish(ctx, *event)
	if err != nil {
		fmt.Printf("Error publishing: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Query published successfully!")
	fmt.Printf("Event ID: %s\n", event.ID)
	printEventJSON(event)

	fmt.Println("\nListening for responses (Ctrl+C to stop)...")

	// Subscribe for responses
	since := nostr.Timestamp(time.Now().Add(-1 * time.Minute).Unix())
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds: []int{30069, 30067}, // Relay directory entries and activities
			Tags:  map[string][]string{"e": {event.ID}},
			Since: &since,
		},
	})
	if err != nil {
		fmt.Printf("Error subscribing: %v\n", err)
		os.Exit(1)
	}
	defer sub.Unsub()

	responseCount := 0
	timeout := time.After(30 * time.Second)

	for {
		select {
		case evt, ok := <-sub.Events:
			if !ok {
				fmt.Println("Subscription closed")
				return
			}
			responseCount++
			fmt.Printf("\n--- Response %d ---\n", responseCount)
			printEventJSON(evt)

		case <-timeout:
			fmt.Printf("\nTimeout reached. Received %d responses.\n", responseCount)
			return

		case <-ctx.Done():
			return
		}
	}
}

func queryHTTP(ctx context.Context, apiURL, targetPubkey, health string) {
	fmt.Printf("Querying discovery service at %s\n", apiURL)

	// Test different endpoints
	endpoints := []string{
		"/health",
		"/api/v1/relays",
	}

	if targetPubkey != "" {
		endpoints = append(endpoints, fmt.Sprintf("/api/v1/pubkey/%s/relays", targetPubkey))
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, endpoint := range endpoints {
		url := apiURL + endpoint
		fmt.Printf("\n=== GET %s ===\n", url)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			fmt.Printf("Error creating request: %v\n", err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		fmt.Printf("Status: %d\n", resp.StatusCode)

		// Pretty print JSON
		var data interface{}
		if err := json.Unmarshal(body, &data); err == nil {
			prettyJSON, _ := json.MarshalIndent(data, "", "  ")
			fmt.Println(string(prettyJSON))
		} else {
			fmt.Println(string(body))
		}
	}

	// Test admin endpoint
	fmt.Printf("\n=== GET %s/admin/dashboard ===\n", apiURL)
	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL+"/admin/dashboard", nil)
	req.Header.Set("X-API-Key", "testkey123")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	fmt.Printf("Status: %d\n", resp.StatusCode)
	var data interface{}
	if err := json.Unmarshal(body, &data); err == nil {
		prettyJSON, _ := json.MarshalIndent(data, "", "  ")
		fmt.Println(string(prettyJSON))
	} else {
		fmt.Println(string(body))
	}
}

func listenForEvents(ctx context.Context, relayURL string) {
	fmt.Printf("Listening for NDP events on %s\n", relayURL)
	fmt.Println("Event kinds: 30066 (Inventory), 30067 (Activity), 30068 (Query), 30069 (Directory)")
	fmt.Println("Press Ctrl+C to stop\n")

	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		fmt.Printf("Error connecting to relay: %v\n", err)
		os.Exit(1)
	}
	defer relay.Close()

	since := nostr.Timestamp(time.Now().Add(-5 * time.Minute).Unix())
	sub, err := relay.Subscribe(ctx, []nostr.Filter{
		{
			Kinds: []int{30066, 30067, 30068, 30069},
			Since: &since,
		},
	})
	if err != nil {
		fmt.Printf("Error subscribing: %v\n", err)
		os.Exit(1)
	}
	defer sub.Unsub()

	eventCount := 0
	for {
		select {
		case evt, ok := <-sub.Events:
			if !ok {
				fmt.Println("Subscription closed")
				return
			}
			eventCount++
			kindName := getKindName(evt.Kind)
			fmt.Printf("\n=== Event %d: %s (kind %d) ===\n", eventCount, kindName, evt.Kind)
			fmt.Printf("ID: %s\n", evt.ID)
			fmt.Printf("Author: %s\n", evt.PubKey[:16]+"...")
			fmt.Printf("Created: %s\n", time.Unix(int64(evt.CreatedAt), 0).Format(time.RFC3339))
			printEventJSON(evt)

		case <-ctx.Done():
			fmt.Printf("\nReceived %d events total.\n", eventCount)
			return
		}
	}
}

func getKindName(kind int) string {
	switch kind {
	case 30066:
		return "Relay Inventory"
	case 30067:
		return "Activity Announcement"
	case 30068:
		return "Discovery Query"
	case 30069:
		return "Relay Directory Entry"
	default:
		return "Unknown"
	}
}

func printEventJSON(event *nostr.Event) {
	data, _ := json.MarshalIndent(event, "", "  ")
	fmt.Println(string(data))
}
