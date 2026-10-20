package api

import (
	"context"
	"sync"
	"testing"
	"time"
)

// go-nostr v0.52.3: Relay.Close() cancels the connection context and then
// reads r.Connection, while the relay's writer goroutine sets r.Connection to
// nil as soon as that context is done. Unsynchronized, so a close at the wrong
// moment is a data race (and a nil dereference inside Close). CI's -race run
// caught it on !27. The go.mod replace points at our fork that drops the nil
// assignment; this test fails under -race without it.
func TestFetchContactsFromRelay_CloseIsRaceFree(t *testing.T) {
	url := mutedRelay(t)

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			// The muted relay never answers, so every call ends on ctx and
			// closes the relay connection from its defer.
			if _, err := fetchContactsFromRelay(ctx, url, "32e1827635450ebb3c5a7d12c1f8e7b2b514439ac10a67eef3d9fd9c5c68e245"); err == nil {
				t.Error("a relay that never answers must not count as answered")
			}
		}()
	}
	wg.Wait()
}
