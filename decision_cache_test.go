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

func TestDecisionCacheStaleWriteCannotRegressGeneration(t *testing.T) {
	cache := newDecisionCache(2, time.Minute)
	currentDecision := cacheEntry{allowed: true, stateCode: "WA"}
	cache.set("203.0.113.25", 2, currentDecision)
	currentElement := cache.entries["203.0.113.25"]

	cache.set("198.51.100.40", 1, cacheEntry{allowed: false, stateCode: "CA"})

	if cache.databaseVersion != 2 {
		t.Fatalf("database version = %d, want 2", cache.databaseVersion)
	}
	if cache.entries["203.0.113.25"] != currentElement {
		t.Fatal("stale write cleared the current generation")
	}
	if _, found := cache.entries["198.51.100.40"]; found {
		t.Fatal("stale write was stored")
	}
	if decision, found := cache.get("203.0.113.25", 2); !found || decision != currentDecision {
		t.Fatalf("current decision = %+v/%t, want %+v/true", decision, found, currentDecision)
	}
}

func TestDecisionCacheStaleReadCannotClearCurrentGeneration(t *testing.T) {
	cache := newDecisionCache(2, time.Minute)
	currentDecision := cacheEntry{allowed: false, stateCode: "CA"}
	cache.set("203.0.113.25", 2, currentDecision)
	currentElement := cache.entries["203.0.113.25"]

	if _, found := cache.get("203.0.113.25", 1); found {
		t.Fatal("stale read returned a current-generation decision")
	}
	if cache.databaseVersion != 2 {
		t.Fatalf("database version = %d, want 2", cache.databaseVersion)
	}
	if cache.entries["203.0.113.25"] != currentElement {
		t.Fatal("stale read cleared the current generation")
	}
	if decision, found := cache.get("203.0.113.25", 2); !found || decision != currentDecision {
		t.Fatalf("current decision = %+v/%t, want %+v/true", decision, found, currentDecision)
	}
}

func TestDecisionCacheNewerGenerationInvalidatesOnceForSameKey(t *testing.T) {
	cache := newDecisionCache(2, time.Minute)
	key := "203.0.113.25"
	cache.set(key, 1, cacheEntry{allowed: false, stateCode: "CA"})

	if _, found := cache.get(key, 2); found {
		t.Fatal("new generation returned the previous generation's decision")
	}

	currentDecision := cacheEntry{allowed: true, stateCode: "WA"}
	cache.set(key, 2, currentDecision)
	if decision, found := cache.get(key, 2); !found || decision != currentDecision {
		t.Fatalf("same-generation decision = %+v/%t, want %+v/true", decision, found, currentDecision)
	}
	if cache.databaseVersion != 2 || len(cache.entries) != 1 {
		t.Fatalf("cache version/size = %d/%d, want 2/1", cache.databaseVersion, len(cache.entries))
	}
}

func TestDecisionCacheGenerationZeroRemainsValidAndMonotonic(t *testing.T) {
	cache := newDecisionCache(2, time.Minute)
	cache.set("zero", 0, cacheEntry{allowed: true})
	if _, found := cache.get("zero", 0); !found {
		t.Fatal("generation zero decision was not stored")
	}

	cache.set("one", 1, cacheEntry{allowed: true})
	cache.set("stale-zero", 0, cacheEntry{allowed: false})
	if cache.databaseVersion != 1 {
		t.Fatalf("database version = %d, want 1", cache.databaseVersion)
	}
	if _, found := cache.get("one", 1); !found {
		t.Fatal("stale generation-zero write cleared generation one")
	}
	if _, found := cache.entries["stale-zero"]; found {
		t.Fatal("stale generation-zero write was stored")
	}
}

func TestDecisionCacheStaleOperationsPreserveTTLAndLRU(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	nowCalls := 0
	cache := newDecisionCache(2, time.Minute)
	cache.now = func() time.Time {
		nowCalls++
		return now
	}
	cache.set("first", 2, cacheEntry{allowed: true})
	cache.set("second", 2, cacheEntry{allowed: false, stateCode: "CA"})

	firstElement := cache.entries["first"]
	secondElement := cache.entries["second"]
	firstItem := firstElement.Value.(decisionCacheItem)
	secondItem := secondElement.Value.(decisionCacheItem)
	front := cache.recency.Front()
	back := cache.recency.Back()
	baselineNowCalls := nowCalls

	if _, found := cache.get("first", 1); found {
		t.Fatal("stale read returned an entry")
	}
	cache.set("second", 1, cacheEntry{allowed: true})

	if nowCalls != baselineNowCalls {
		t.Fatalf("stale operations called the TTL clock %d times, want %d", nowCalls, baselineNowCalls)
	}
	if cache.entries["first"] != firstElement || cache.entries["second"] != secondElement {
		t.Fatal("stale operations replaced current-generation entries")
	}
	if cache.recency.Front() != front || cache.recency.Back() != back {
		t.Fatal("stale operations changed LRU order")
	}
	if cache.entries["first"].Value.(decisionCacheItem) != firstItem ||
		cache.entries["second"].Value.(decisionCacheItem) != secondItem {
		t.Fatal("stale operations changed cached decision or expiry")
	}

	cache.set("third", 2, cacheEntry{allowed: true})
	if _, found := cache.entries["first"]; found {
		t.Fatal("stale read refreshed the least-recently-used entry")
	}
	if _, found := cache.entries["second"]; !found {
		t.Fatal("stale write caused an unexpected eviction")
	}
}

func TestDisabledDecisionCacheDoesNotStoreEntries(t *testing.T) {
	cache := newDecisionCache(0, time.Minute)
	cache.set("203.0.113.25", 2, cacheEntry{allowed: true})
	cache.set("198.51.100.40", 1, cacheEntry{allowed: false})

	if _, found := cache.get("203.0.113.25", 2); found {
		t.Fatal("disabled cache returned a stored entry")
	}
	if _, found := cache.get("198.51.100.40", 1); found {
		t.Fatal("disabled cache returned a stale entry")
	}
	if cache.databaseVersion != 0 || len(cache.entries) != 0 || cache.recency.Len() != 0 {
		t.Fatalf(
			"disabled cache version/entries/recency = %d/%d/%d, want 0/0/0",
			cache.databaseVersion,
			len(cache.entries),
			cache.recency.Len(),
		)
	}
}

func TestDecisionCacheKeepsDistinctIPv6Addresses(t *testing.T) {
	cache := newDecisionCache(2, time.Minute)
	cache.set("2001:db8:abcd:1::1", 1, cacheEntry{allowed: true})

	if _, found := cache.get("2001:db8:abcd:1::2", 1); found {
		t.Fatal("distinct IPv6 address unexpectedly shared a cached decision")
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
