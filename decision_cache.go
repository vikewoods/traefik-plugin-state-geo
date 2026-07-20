package traefik_plugin_state_geo

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

const (
	defaultDecisionCacheSize = 1_000
	maximumDecisionCacheSize = 100_000
	defaultDecisionCacheTTL  = 15 * time.Minute
)

type decisionCacheItem struct {
	key       string
	decision  cacheEntry
	expiresAt time.Time
}

type decisionCache struct {
	mutex           sync.Mutex
	entries         map[string]*list.Element
	recency         list.List
	capacity        int
	ttl             time.Duration
	databaseVersion uint64
	now             func() time.Time
}

func parseDecisionCacheConfig(size int, rawTTL string) (int, time.Duration, error) {
	if size < 0 || size > maximumDecisionCacheSize {
		return 0, 0, fmt.Errorf(
			"invalid cacheSize %d: expected a value from 0 to %d",
			size,
			maximumDecisionCacheSize,
		)
	}

	ttl := defaultDecisionCacheTTL
	if rawTTL != "" {
		parsedTTL, err := time.ParseDuration(rawTTL)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid cacheTTL %q: %w", rawTTL, err)
		}
		ttl = parsedTTL
	}
	if size > 0 && ttl <= 0 {
		return 0, 0, fmt.Errorf("invalid cacheTTL %q: expected a positive duration", rawTTL)
	}

	return size, ttl, nil
}

func newDecisionCache(capacity int, ttl time.Duration) *decisionCache {
	return &decisionCache{
		entries:  make(map[string]*list.Element, capacity),
		capacity: capacity,
		ttl:      ttl,
		now:      time.Now,
	}
}

func (c *decisionCache) get(key string, databaseVersion uint64) (cacheEntry, bool) {
	if c.capacity == 0 {
		return cacheEntry{}, false
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	if !c.acceptVersion(databaseVersion) {
		return cacheEntry{}, false
	}

	element, exists := c.entries[key]
	if !exists {
		return cacheEntry{}, false
	}

	item, ok := element.Value.(decisionCacheItem)
	if !ok {
		delete(c.entries, key)
		c.recency.Remove(element)
		return cacheEntry{}, false
	}
	if !c.now().Before(item.expiresAt) {
		c.remove(key, element)
		return cacheEntry{}, false
	}

	c.recency.MoveToFront(element)
	return item.decision, true
}

func (c *decisionCache) set(key string, databaseVersion uint64, decision cacheEntry) {
	if c.capacity == 0 {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	if !c.acceptVersion(databaseVersion) {
		return
	}

	expiresAt := c.now().Add(c.ttl)
	if element, exists := c.entries[key]; exists {
		element.Value = decisionCacheItem{
			key:       key,
			decision:  decision,
			expiresAt: expiresAt,
		}
		c.recency.MoveToFront(element)
		return
	}

	element := c.recency.PushFront(decisionCacheItem{
		key:       key,
		decision:  decision,
		expiresAt: expiresAt,
	})
	c.entries[key] = element

	if c.recency.Len() > c.capacity {
		oldest := c.recency.Back()
		item, ok := oldest.Value.(decisionCacheItem)
		if !ok {
			c.recency.Remove(oldest)
			return
		}
		c.remove(item.key, oldest)
	}
}

func (c *decisionCache) acceptVersion(databaseVersion uint64) bool {
	if databaseVersion < c.databaseVersion {
		// A stale in-flight request can finish after a newer immutable
		// database snapshot has already been published.
		return false
	}
	if databaseVersion == c.databaseVersion {
		return true
	}

	c.entries = make(map[string]*list.Element, c.capacity)
	c.recency.Init()
	c.databaseVersion = databaseVersion
	return true
}

func (c *decisionCache) remove(key string, element *list.Element) {
	delete(c.entries, key)
	c.recency.Remove(element)
}
