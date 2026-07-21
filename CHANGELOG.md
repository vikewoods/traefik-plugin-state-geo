# Changelog

All notable changes are documented here. The project follows Semantic
Versioning for released tags.

## Unreleased

## v1.2.0-rc.1 - 2026-07-21

### Compatibility

- Restored the installable repository-root module identity,
  `github.com/vikewoods/traefik-plugin-state-geo`. Traefik's public plugin
  registry currently misinterprets the Go `/v2` semantic-import suffix as a
  GitHub repository name.
- Republished the hardened middleware as a v1 compatibility prerelease. This
  release contains breaking behavior changes from v1.1; operators must follow
  `docs/migration-v1.md` and validate a canary before rollout.
- Updated catalog metadata, examples, smoke tests, and release validation for
  `v1.2.0-rc.1`.

## v2.0.0-rc.2 - 2026-07-20 (withdrawn)

### Added

- Documented and tested native and Traefik-interpreted compatibility with
  compact stategeodb `StateGeo-Country-USSubdivision` artifacts while
  preserving GeoLite2-City and GeoIP2-City support.

### Fixed

- Decision-cache generations are monotonic across concurrent database reloads.
  Stale in-flight reads now miss and stale writes are discarded without
  regressing the active generation or clearing current decisions.

## v2.0.0-rc.1 - 2026-07-20 (withdrawn)

### Breaking

- The Go module, Traefik manifest import, and static `moduleName` used the Go
  `/v2` semantic-import suffix. This was valid Go module versioning but proved
  incompatible with Traefik's public plugin registry, so the prerelease was
  withdrawn.
- `clientIPHeaders` now defaults to only `X-Forwarded-For`; Cloudflare,
  True-Client-IP, RFC `Forwarded`, X-Real-IP, and custom headers are opt-in.
- Present-but-invalid headers from trusted peers are rejected by default via
  the new `rejectInvalidClientIPHeaders` setting and `invalidClientIPPolicy`.
- `invalidClientIPPolicy` and `privateIPPolicy` now default to `deny`.
- `logLevel` now defaults to `info` and records deny decisions.

### Added

- Interpreted-plugin CI coverage for both Traefik v3.7.1 and v3.7.6.
- Privacy-safe warnings for malformed trusted client-IP headers.
- Link-local and unspecified client addresses are governed by
  `privateIPPolicy`.
- Concurrent reload regression coverage and serial/parallel snapshot
  benchmarks.

### Fixed

- v2 tags could be resolved by the Go module proxy and local Yaegi loader, but
  Traefik's public registry could not retrieve the dependency-bearing plugin
  from GitHub because it treated `/v2` as the repository name.
- Requests no longer queue behind full MMDB reads during an atomic database
  reload; one request performs the reload while others retain the current
  immutable reader. Atomic deadlines and immutable snapshot publication also
  remove steady-state reload/reader locks from the request path.
- MMDB lookup errors remain available in logs even when client-IP logging is
  disabled.
- IP whitelist checks use the already parsed address instead of reparsing its
  string representation.

## v2.0.0-alpha - 2026-07-19 (withdrawn)

### Breaking

- Forwarding headers are trusted only from configured `trustedProxyCIDRs`;
  without trusted peers, the middleware uses `RemoteAddr`.
- `whitelistedPaths` now matches exact normalized paths. Move existing prefix
  rules to `whitelistedPathPrefixes`.
- Invalid IP/CIDR, state, path, header, duration, policy, and template settings
  now fail Middleware construction instead of being silently skipped.
- The production-style GeoLite2 database is no longer shipped in repository
  archives. Operators must mount their own current City MMDB.

### Added

- Apache License 2.0 for the project, with preserved third-party notices.
- Ordered `clientIPHeaders` with Cloudflare, True Client IP, XFF, RFC
  `Forwarded`, X-Real-IP, custom header, IPv4, and IPv6 support.
- Explicit database, lookup, invalid-IP, unknown-country,
  unknown-subdivision, and private-IP policies.
- Shared per-process MMDB readers, request-driven atomic replacement reload,
  last-known-good fallback, and generation-aware caches.
- Bounded TTL/LRU decision cache with configurable size and TTL.
- Segment-safe path-prefix bypasses and IPv4/IPv6 CIDR allowlists.
- Escaped, size-limited block templates and structured configurable logging.
- Deterministic MaxMind fixture integration tests, fuzz targets, benchmarks,
  race tests, lint/security automation, CodeQL, Dependabot, and a Traefik
  v3.7.1 interpreted-plugin smoke test.
- Kubernetes Helm/PVC, CRD, canary, rollback, migration, and release
  documentation.

### Fixed

- Header spoofing from untrusted direct peers.
- XFF chain selection when Traefik appends trusted proxy hops.
- Default `X-Real-IP` priority masking the useful XFF chain after Traefik
  rewrites `X-Real-IP` to the socket peer.
- Yaegi incompatibility with the Go `clear` built-in.
- Stale database decisions after weekly atomic MMDB replacement.
- Raw path-prefix matches such as `/health` unintentionally matching
  `/health-attack`.
- Unescaped dynamic state values in custom HTML responses.

## v1.1.2 - 2026-04-28

- Added exact IP and CIDR whitelist support.

Earlier v1 tags predate this changelog. Consult Git history for their changes.
