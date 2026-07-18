# Changelog

All notable changes are documented here. The project follows Semantic
Versioning for released tags.

## Unreleased

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
