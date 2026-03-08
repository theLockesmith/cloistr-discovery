package relay

import (
	"context"
	"testing"
	"time"
)

func TestDNSCache_SuccessResult(t *testing.T) {
	cfg := DNSCacheConfig{
		SuccessTTL:   100 * time.Millisecond,
		NXDomainTTL:  time.Hour,
		TimeoutTTL:   time.Hour,
		ErrorTTL:     time.Hour,
		MaxBackoffMs: 300000,
	}
	cache := NewDNSCache(cfg)

	hostname := "relay.example.com"

	// Initially no result
	result := cache.Get(hostname)
	if result != nil {
		t.Error("expected nil for uncached hostname")
	}

	// Set success
	cache.SetSuccess(hostname)

	// Should be cached
	result = cache.Get(hostname)
	if result == nil {
		t.Fatal("expected cached result")
	}
	if result.Type != DNSResultSuccess {
		t.Errorf("Type = %v, want DNSResultSuccess", result.Type)
	}

	// ShouldSkip should return false for success
	skip, _, _ := cache.ShouldSkip(hostname)
	if skip {
		t.Error("ShouldSkip should return false for success results")
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	result = cache.Get(hostname)
	if result != nil {
		t.Error("expected nil for expired result")
	}
}

func TestDNSCache_NXDomainResult(t *testing.T) {
	cfg := DNSCacheConfig{
		SuccessTTL:   time.Hour,
		NXDomainTTL:  100 * time.Millisecond,
		TimeoutTTL:   time.Hour,
		ErrorTTL:     time.Hour,
		MaxBackoffMs: 300000,
	}
	cache := NewDNSCache(cfg)

	hostname := "dead.relay.com"

	cache.SetNXDomain(hostname, "no such host")

	result := cache.Get(hostname)
	if result == nil {
		t.Fatal("expected cached result")
	}
	if result.Type != DNSResultNXDomain {
		t.Errorf("Type = %v, want DNSResultNXDomain", result.Type)
	}
	if result.Error != "no such host" {
		t.Errorf("Error = %q, want %q", result.Error, "no such host")
	}

	// ShouldSkip should return true for NXDOMAIN
	skip, resultType, reason := cache.ShouldSkip(hostname)
	if !skip {
		t.Error("ShouldSkip should return true for NXDOMAIN")
	}
	if resultType != DNSResultNXDomain {
		t.Errorf("resultType = %v, want DNSResultNXDomain", resultType)
	}
	if reason != "no such host" {
		t.Errorf("reason = %q, want %q", reason, "no such host")
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Should be expired, so ShouldSkip returns false
	skip, _, _ = cache.ShouldSkip(hostname)
	if skip {
		t.Error("ShouldSkip should return false after expiry")
	}
}

func TestDNSCache_TimeoutWithBackoff(t *testing.T) {
	cfg := DNSCacheConfig{
		SuccessTTL:   time.Hour,
		NXDomainTTL:  time.Hour,
		TimeoutTTL:   50 * time.Millisecond, // Base timeout TTL
		ErrorTTL:     time.Hour,
		MaxBackoffMs: 200, // 200ms max
	}
	cache := NewDNSCache(cfg)

	hostname := "slow.relay.com"

	// First timeout - should use base TTL (50ms)
	cache.SetTimeout(hostname, "timeout 1")
	result := cache.Get(hostname)
	if result == nil {
		t.Fatal("expected cached result")
	}
	if result.BackoffCnt != 0 {
		t.Errorf("BackoffCnt = %d, want 0", result.BackoffCnt)
	}

	// Expiry should be ~50ms from now
	expectedExpiry := result.CachedAt.Add(50 * time.Millisecond)
	diff := result.Expiry.Sub(expectedExpiry)
	if diff < -5*time.Millisecond || diff > 5*time.Millisecond {
		t.Errorf("First timeout expiry off by %v", diff)
	}

	// Second timeout - should double the TTL (100ms)
	cache.SetTimeout(hostname, "timeout 2")
	result = cache.Get(hostname)
	if result.BackoffCnt != 1 {
		t.Errorf("BackoffCnt = %d, want 1", result.BackoffCnt)
	}
	expectedExpiry = result.CachedAt.Add(100 * time.Millisecond)
	diff = result.Expiry.Sub(expectedExpiry)
	if diff < -5*time.Millisecond || diff > 5*time.Millisecond {
		t.Errorf("Second timeout expiry off by %v", diff)
	}

	// Third timeout - should be capped at max (200ms)
	cache.SetTimeout(hostname, "timeout 3")
	result = cache.Get(hostname)
	if result.BackoffCnt != 2 {
		t.Errorf("BackoffCnt = %d, want 2", result.BackoffCnt)
	}
	// 50 * 2^2 = 200ms, at the cap
	expectedExpiry = result.CachedAt.Add(200 * time.Millisecond)
	diff = result.Expiry.Sub(expectedExpiry)
	if diff < -5*time.Millisecond || diff > 5*time.Millisecond {
		t.Errorf("Third timeout expiry off by %v", diff)
	}

	// Fourth timeout - should still be capped at max (200ms)
	cache.SetTimeout(hostname, "timeout 4")
	result = cache.Get(hostname)
	// 50 * 2^3 = 400ms, but capped at 200ms
	expectedExpiry = result.CachedAt.Add(200 * time.Millisecond)
	diff = result.Expiry.Sub(expectedExpiry)
	if diff < -5*time.Millisecond || diff > 5*time.Millisecond {
		t.Errorf("Fourth timeout expiry should be capped, off by %v", diff)
	}
}

func TestDNSCache_ErrorResult(t *testing.T) {
	cfg := DNSCacheConfig{
		SuccessTTL:   time.Hour,
		NXDomainTTL:  time.Hour,
		TimeoutTTL:   time.Hour,
		ErrorTTL:     100 * time.Millisecond,
		MaxBackoffMs: 300000,
	}
	cache := NewDNSCache(cfg)

	hostname := "error.relay.com"

	cache.SetError(hostname, "connection refused")

	result := cache.Get(hostname)
	if result == nil {
		t.Fatal("expected cached result")
	}
	if result.Type != DNSResultError {
		t.Errorf("Type = %v, want DNSResultError", result.Type)
	}

	skip, resultType, _ := cache.ShouldSkip(hostname)
	if !skip {
		t.Error("ShouldSkip should return true for error results")
	}
	if resultType != DNSResultError {
		t.Errorf("resultType = %v, want DNSResultError", resultType)
	}
}

func TestDNSCache_Cleanup(t *testing.T) {
	cfg := DNSCacheConfig{
		SuccessTTL:   50 * time.Millisecond,
		NXDomainTTL:  50 * time.Millisecond,
		TimeoutTTL:   50 * time.Millisecond,
		ErrorTTL:     50 * time.Millisecond,
		MaxBackoffMs: 300000,
	}
	cache := NewDNSCache(cfg)

	// Add several entries
	cache.SetSuccess("success.com")
	cache.SetNXDomain("nxdomain.com", "no such host")
	cache.SetTimeout("timeout.com", "timeout")
	cache.SetError("error.com", "error")

	if cache.Size() != 4 {
		t.Errorf("Size = %d, want 4", cache.Size())
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Cleanup
	cache.Cleanup(context.Background())

	if cache.Size() != 0 {
		t.Errorf("Size after cleanup = %d, want 0", cache.Size())
	}
}

func TestDNSCache_Stats(t *testing.T) {
	cfg := DefaultDNSCacheConfig()
	cache := NewDNSCache(cfg)

	cache.SetSuccess("success1.com")
	cache.SetSuccess("success2.com")
	cache.SetNXDomain("nxdomain.com", "no such host")
	cache.SetTimeout("timeout.com", "timeout")
	cache.SetError("error.com", "error")

	stats := cache.Stats()

	if stats.Total != 5 {
		t.Errorf("Total = %d, want 5", stats.Total)
	}
	if stats.Success != 2 {
		t.Errorf("Success = %d, want 2", stats.Success)
	}
	if stats.NXDomain != 1 {
		t.Errorf("NXDomain = %d, want 1", stats.NXDomain)
	}
	if stats.Timeout != 1 {
		t.Errorf("Timeout = %d, want 1", stats.Timeout)
	}
	if stats.Error != 1 {
		t.Errorf("Error = %d, want 1", stats.Error)
	}
}

func TestDNSCache_ShouldSkipUncached(t *testing.T) {
	cfg := DefaultDNSCacheConfig()
	cache := NewDNSCache(cfg)

	skip, _, _ := cache.ShouldSkip("uncached.com")
	if skip {
		t.Error("ShouldSkip should return false for uncached hostname")
	}
}
