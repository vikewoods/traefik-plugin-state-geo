# GeoIP database lifecycle and decision cache

## Process-level database sharing

The vendored Yaegi-compatible MaxMind reader loads the complete MMDB into a Go byte slice. Loading one reader per Middleware resource would therefore multiply the roughly 65.9 MB live database by every Middleware instance in each Traefik pod.

The plugin now keeps a process-level registry keyed by the normalized absolute `dbPath`. Every Middleware using the same path receives the same `databaseManager` and immutable reader. In the audited deployment this means one full database copy per Traefik process, not one copy per Middleware resource. Each of the three Traefik pods still owns its own process memory, as expected.

The registry intentionally lives for the Traefik process lifetime because the Traefik middleware plugin API has no matching lifecycle callback for releasing an individual Middleware. The number of retained managers is bounded by the number of unique configured database paths, which operators should keep small.

## Request-driven hot reload

No background goroutine is used. The first eligible request after `databaseReloadInterval` performs a serialized file check; all other requests continue normally. The default interval is one minute, and values below one second are rejected.

```yaml
dbPath: /data/geolite/GeoLite2-City.mmdb
databaseReloadInterval: 1m
```

The manager compares file size and modification time. When they change, it opens and validates a new reader before swapping the immutable reader pointer and incrementing the database generation. This matches the audited updater's temporary-file-plus-atomic-rename workflow.

If multiple Middleware instances share a path but request different reload intervals, the shared manager uses the shortest interval. This avoids duplicate readers while honoring the strictest freshness requirement.

### Failure behavior

- Initial load failure is handled by `databaseFailurePolicy`.
- A manager whose initial load failed retries after the reload interval, allowing recovery without Traefik restart.
- A reload failure retains the last known-good reader and generation.
- The reload error is returned only to the request that performed the interval check, bounding the current diagnostic to at most once per interval and database path.
- A successful swap changes the generation and invalidates Middleware decision caches.

The current pure-Go reader has no OS mapping to close. An in-flight request holds an interface reference to its immutable reader snapshot, so the old byte slice remains alive until that request finishes and is then garbage collected. If the vendored reader is later changed back to mmap, reader leasing and `Close` behavior must be reevaluated before that upgrade is accepted.

## Bounded TTL/LRU decision cache

The previous cache stopped accepting new entries after 1,000 unique IPs and never expired or evicted old decisions. It has been replaced with a concurrency-safe least-recently-used cache.

```yaml
cacheSize: 1000
cacheTTL: 15m
```

Behavior:

- `cacheSize: 0` disables caching.
- Maximum size is 100,000 entries to prevent accidental unbounded allocation.
- Enabled caches require a positive TTL.
- Access refreshes recency but does not extend TTL; `set` refreshes TTL.
- The least-recently-used entry is evicted at capacity.
- A database generation change clears the cache before the next read or write.
- Lookup errors and database-unavailable decisions are not cached.
- Private-address allow/deny shortcuts are not cached.

Each Middleware has its own small decision cache because its blocked states, policies, and whitelists may differ. Only the large immutable database reader is shared.

## Concurrency model

- A registry mutex protects manager creation.
- Each manager serializes stat/open/swap checks with one reload mutex.
- A separate reader mutex protects only the pointer, fingerprint, and generation snapshot.
- Disk I/O and database parsing occur outside the reader lock, so lookups continue on the last good reader during a replacement open.
- The cache uses a short mutex around its map/list invariants.
- No goroutine, timer, or channel is retained by the feature.

Race-detector tests exercise concurrent cache access and concurrent manager snapshots/reloads.
