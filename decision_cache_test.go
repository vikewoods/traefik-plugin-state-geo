package traefik_plugin_state_geo

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestDecisionCacheLRUEvictionAndExpiry(t *testing.T) {
	now := time.Date(2026, time.July, 18, 20, 0, 0, 0, time.UTC)
	cache := newDecisionCache(2, time.Minute)
	cache.now = func() time.Time { return now }

	allowed := cacheEntry{allowed: true}
	blocked := cacheEntry{allowed: false, stateCode: "CA"}
	cache.set("first", 1, allowed)
	cache.set("second", 1, blocked)

	if _, found := cache.get("first", 1); !found {
		t.Fatal("expected first entry to be present")
	}

	cache.set("third", 1, allowed)
	if _, found := cache.get("second", 1); found {
		t.Fatal("expected least-recently-used second entry to be evicted")
	}
	if _, found := cache.get("first", 1); !found {
		t.Fatal("expected recently used first entry to remain")
	}
	if _, found := cache.get("third", 1); !found {
		t.Fatal("expected third entry to be present")
	}

	now = now.Add(time.Minute)
	if _, found := cache.get("first", 1); found {
		t.Fatal("expected entry to expire at its TTL boundary")
	}
}

func TestDecisionCacheInvalidatesOnDatabaseVersionChange(t *testing.T) {
	cache := newDecisionCache(2, time.Minute)
	cache.set("203.0.113.25", 1, cacheEntry{allowed: false, stateCode: "CA"})

	if _, found := cache.get("203.0.113.25", 1); !found {
		t.Fatal("expected entry for database version 1")
	}
	if _, found := cache.get("203.0.113.25", 2); found {
		t.Fatal("expected database version change to invalidate cached decision")
	}
}

func TestDisabledDecisionCacheDoesNotStoreEntries(t *testing.T) {
	cache := newDecisionCache(0, time.Minute)
	cache.set("203.0.113.25", 1, cacheEntry{allowed: true})

	if _, found := cache.get("203.0.113.25", 1); found {
		t.Fatal("disabled cache returned a stored entry")
	}
}

func TestDecisionCacheRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		size int
		ttl  string
	}{
		{name: "negative size", size: -1, ttl: "1m"},
		{name: "size above maximum", size: maximumDecisionCacheSize + 1, ttl: "1m"},
		{name: "invalid ttl", size: 1, ttl: "not-a-duration"},
		{name: "zero ttl when enabled", size: 1, ttl: "0s"},
		{name: "negative ttl when enabled", size: 1, ttl: "-1s"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := parseDecisionCacheConfig(test.size, test.ttl); err == nil {
				t.Fatal("parseDecisionCacheConfig() error = nil, want configuration error")
			}
		})
	}
}

func TestDecisionCacheConcurrentAccess(_ *testing.T) {
	cache := newDecisionCache(128, time.Minute)

	var waitGroup sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			for iteration := 0; iteration < 200; iteration++ {
				key := fmt.Sprintf("%d-%d", worker, iteration%32)
				cache.set(key, uint64(iteration%3), cacheEntry{allowed: iteration%2 == 0})
				_, _ = cache.get(key, uint64(iteration%3))
			}
		}(worker)
	}
	waitGroup.Wait()
}
