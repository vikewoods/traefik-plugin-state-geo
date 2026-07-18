# Phase 3 benchmark snapshot

Date: 2026-07-18  
Machine: Apple M4 Max, Darwin/arm64  
Go benchmark parallelism suffix: 16

Command:

```text
go test -run '^$' -bench='Benchmark(ClientIPResolver|DecisionCacheHit|GeoLookup)$' -benchmem -count=5 ./...
```

These are post-implementation baselines, not before/after proof. Five repetitions were used to show run-to-run stability. Releases that change hot-path code should compare a new benchmark run with this snapshot.

| Benchmark | Observed range | Bytes/op | Allocs/op |
| --- | --- | --- | --- |
| Client IP resolver, trusted Cloudflare IPv6 path | 94.5-103.3 ns/op | 48 | 1 |
| Decision cache hit | 37.0-37.6 ns/op | 0 | 0 |
| MaxMind IPv6 lookup against bundled DB | 173.6-174.9 ns/op | 0 | 0 |

The client resolver allocation comes from retrieving HTTP header values on the selected path. It is small relative to middleware and proxy request handling; no speculative micro-optimization is justified from this baseline. The bounded cache and direct MaxMind lookup paths were allocation-free in this benchmark.

The dominant memory concern is structural rather than per-lookup allocation: the pure-Go reader holds the entire MMDB. Process-level sharing removes multiplication by Middleware count while preserving one database copy per Traefik pod.
