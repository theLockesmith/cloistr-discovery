// Package relay handles relay monitoring and health checking.
package relay

import (
	"context"
	"sync"
	"time"
)

// DNSResultType represents the type of DNS lookup result.
type DNSResultType int

const (
	DNSResultSuccess DNSResultType = iota
	DNSResultNXDomain
	DNSResultTimeout
	DNSResultError
)

// DNSResult represents a cached DNS lookup result.
type DNSResult struct {
	Type       DNSResultType
	Error      string
	CachedAt   time.Time
	Expiry     time.Time
	BackoffCnt int // For timeout results, tracks retry backoff
}

// DNSCache caches DNS lookup results to reduce load.
type DNSCache struct {
	mu sync.RWMutex

	// Cache of hostname -> result
	results map[string]*DNSResult

	// TTL configuration
	successTTL   time.Duration
	nxdomainTTL  time.Duration
	timeoutTTL   time.Duration
	errorTTL     time.Duration
	maxBackoffMs int // Max backoff for timeout retries (in milliseconds)
}

// DNSCacheConfig holds configuration for the DNS cache.
type DNSCacheConfig struct {
	SuccessTTL   time.Duration
	NXDomainTTL  time.Duration
	TimeoutTTL   time.Duration
	ErrorTTL     time.Duration
	MaxBackoffMs int
}

// DefaultDNSCacheConfig returns sensible defaults.
func DefaultDNSCacheConfig() DNSCacheConfig {
	return DNSCacheConfig{
		SuccessTTL:   1 * time.Hour,
		NXDomainTTL:  24 * time.Hour,
		TimeoutTTL:   30 * time.Minute,
		ErrorTTL:     15 * time.Minute,
		MaxBackoffMs: 300000, // 5 minutes max
	}
}

// NewDNSCache creates a new DNS cache with the given configuration.
func NewDNSCache(cfg DNSCacheConfig) *DNSCache {
	return &DNSCache{
		results:      make(map[string]*DNSResult),
		successTTL:   cfg.SuccessTTL,
		nxdomainTTL:  cfg.NXDomainTTL,
		timeoutTTL:   cfg.TimeoutTTL,
		errorTTL:     cfg.ErrorTTL,
		maxBackoffMs: cfg.MaxBackoffMs,
	}
}

// Get retrieves a cached result for a hostname.
// Returns nil if not cached or expired.
func (c *DNSCache) Get(hostname string) *DNSResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result, ok := c.results[hostname]
	if !ok {
		return nil
	}

	// Check expiry
	if time.Now().After(result.Expiry) {
		return nil
	}

	return result
}

// SetSuccess caches a successful DNS lookup.
func (c *DNSCache) SetSuccess(hostname string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.results[hostname] = &DNSResult{
		Type:     DNSResultSuccess,
		CachedAt: now,
		Expiry:   now.Add(c.successTTL),
	}
}

// SetNXDomain caches an NXDOMAIN (no such host) result.
func (c *DNSCache) SetNXDomain(hostname string, errorMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.results[hostname] = &DNSResult{
		Type:     DNSResultNXDomain,
		Error:    errorMsg,
		CachedAt: now,
		Expiry:   now.Add(c.nxdomainTTL),
	}
}

// SetTimeout caches a timeout result with exponential backoff.
func (c *DNSCache) SetTimeout(hostname string, errorMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Get existing backoff count
	backoffCnt := 0
	if existing, ok := c.results[hostname]; ok && existing.Type == DNSResultTimeout {
		backoffCnt = existing.BackoffCnt + 1
	}

	// Calculate backoff: base TTL * 2^backoffCnt, capped at maxBackoffMs
	backoffMs := int(c.timeoutTTL.Milliseconds()) * (1 << backoffCnt)
	if backoffMs > c.maxBackoffMs {
		backoffMs = c.maxBackoffMs
	}

	c.results[hostname] = &DNSResult{
		Type:       DNSResultTimeout,
		Error:      errorMsg,
		CachedAt:   now,
		Expiry:     now.Add(time.Duration(backoffMs) * time.Millisecond),
		BackoffCnt: backoffCnt,
	}
}

// SetError caches a generic error result.
func (c *DNSCache) SetError(hostname string, errorMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.results[hostname] = &DNSResult{
		Type:     DNSResultError,
		Error:    errorMsg,
		CachedAt: now,
		Expiry:   now.Add(c.errorTTL),
	}
}

// ShouldSkip returns true if the hostname should be skipped based on cached result.
// Returns the cached result type and whether to skip.
func (c *DNSCache) ShouldSkip(hostname string) (skip bool, resultType DNSResultType, reason string) {
	result := c.Get(hostname)
	if result == nil {
		return false, DNSResultSuccess, ""
	}

	// Success results don't cause skipping - we want to check the relay
	if result.Type == DNSResultSuccess {
		return false, DNSResultSuccess, ""
	}

	// All other cached results should skip
	return true, result.Type, result.Error
}

// Cleanup removes expired entries from the cache.
// Should be called periodically to prevent memory growth.
func (c *DNSCache) Cleanup(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for hostname, result := range c.results {
		if now.After(result.Expiry) {
			delete(c.results, hostname)
		}
	}
}

// Size returns the number of cached entries.
func (c *DNSCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.results)
}

// Stats returns cache statistics.
type DNSCacheStats struct {
	Total    int
	Success  int
	NXDomain int
	Timeout  int
	Error    int
}

// Stats returns statistics about cached entries.
func (c *DNSCache) Stats() DNSCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := DNSCacheStats{Total: len(c.results)}
	for _, result := range c.results {
		switch result.Type {
		case DNSResultSuccess:
			stats.Success++
		case DNSResultNXDomain:
			stats.NXDomain++
		case DNSResultTimeout:
			stats.Timeout++
		case DNSResultError:
			stats.Error++
		}
	}
	return stats
}
