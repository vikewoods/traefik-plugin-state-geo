# Kubernetes readiness phases

This plan converts the audit into small, independently verifiable changes. A phase is complete only when its implementation, tests, and documentation are complete. Cluster mutations are deliberately excluded until separately authorized.

## Status legend

- `[ ]` planned
- `[-]` in progress
- `[x]` complete

## Phase 0: Preserve research and decisions

- [x] Record the repository audit and upstream references.
- [x] Record the live Traefik, PVC, updater, and traffic evidence.
- [x] Record accepted decisions about committed MaxMind credentials and `externalTrafficPolicy: Cluster`.
- [x] Preserve the Cluster-SNAT/header-authenticity limitation for deployment review.

Evidence: [`research/2026-07-18-traefik-kubernetes-audit.md`](research/2026-07-18-traefik-kubernetes-audit.md)

## Phase 1: Trusted, multi-source client-IP resolution

- [x] Add configurable ordered client-IP header sources.
- [x] Trust headers only when the immediate socket peer matches a configured proxy CIDR.
- [x] Support `CF-Connecting-IP`, `True-Client-IP`, `X-Real-IP`, `X-Forwarded-For`, RFC 7239 `Forwarded`, custom single-IP headers, and direct `RemoteAddr` fallback.
- [x] Parse IPv4, IPv6, IPv4-mapped IPv6, bracketed IPv6, and common address-with-port forms.
- [x] Resolve forwarding chains from right to left and reject malformed or ambiguous input.
- [x] Add table-driven tests for trust, spoofing, precedence, malformed values, chains, and both IP families.
- [x] Integrate the resolver into whitelist and GeoIP decisions.
- [x] Document the required K8s node CIDR configuration and residual SNAT limitation.

Exit checks: `gofmt`, `go test ./...`, `go test -race ./...`, and `go vet ./...`.

Implementation reference: [`client-ip-resolution.md`](client-ip-resolution.md)

## Phase 2: Explicit decision and failure policies

- [x] Replace the broad `failOpen` behavior with clearly documented policies for database load failure, lookup error, invalid client address, unknown country, and unknown subdivision.
- [x] Make the US-without-subdivision outcome configurable instead of implicitly blocked.
- [x] Replace unconditional private/loopback bypass with an explicit policy or whitelist requirement.
- [x] Validate configuration combinations during middleware construction.
- [x] Add a decision table and exhaustive policy tests.

Exit condition: every unresolved or failed input has an operator-selected, tested outcome.

Implementation reference: [`failure-policies.md`](failure-policies.md)

## Phase 3: Database lifecycle, memory, and cache

- [x] Load one database reader per database path instead of per Middleware instance where Yaegi permits safe sharing.
- [x] Detect atomic MMDB replacement and reload without restarting Traefik.
- [x] Keep the last known-good database if reload fails and surface a bounded diagnostic.
- [x] Define reader ownership and safe concurrent swap behavior.
- [x] Replace or remove the fixed 1,000-entry cache; if retained, add bounds, expiry, and invalidation on reload.
- [x] Benchmark lookup, resolver, cache, memory, and reload behavior.
- [x] Retain and verify the current pure-Go reader under Traefik v3.7.1; upstream mmap files remain excluded from the interpreted runtime.

Exit condition: weekly PVC updates become active safely and memory does not scale linearly with Middleware resources.

Implementation references: [`database-lifecycle.md`](database-lifecycle.md) and [`research/2026-07-18-phase3-benchmarks.md`](research/2026-07-18-phase3-benchmarks.md)

## Phase 4: Middleware hardening and observability

- [x] Give path whitelist rules explicit exact/prefix semantics and segment-safe matching.
- [x] Reject empty or invalid path rules that could bypass all traffic.
- [x] Define template size/path/read-failure behavior and escape dynamic values safely.
- [x] Replace unconditional per-request prints with configurable, bounded logging.
- [x] Avoid logging client IPs by default and document privacy implications.
- [x] Normalize whitelist IP/CIDR handling across IPv4 and IPv6.

Exit condition: bypass rules are unambiguous and normal traffic produces no noisy or privacy-sensitive logs.

Implementation reference: [`middleware-hardening.md`](middleware-hardening.md)

## Phase 5: Verification and Traefik interpreter compatibility

- [x] Split deterministic unit tests from real-MMDB integration behavior and remove the mutable production database dependency.
- [x] Create a tiny, redistributable test fixture that covers IPv4, IPv6, US subdivisions, non-US, and unknown data.
- [x] Add fuzz tests for client-IP and configuration parsers.
- [x] Add CI for formatting, tests, race detection, vet, static analysis, vulnerability scanning, and coverage.
- [-] The local manifest/Yaegi checks pass; the catalog analyzer requires a
      committed, pushed semantic release tag.
- [x] Start a local Traefik instance with the plugin and exercise a complete request matrix.
- [x] Test against the same Traefik major/minor version used by the cluster.

Exit condition: native tests and an actual Traefik runtime agree on behavior.

Implementation references: [`research/2026-07-18-traefik-runtime-validation.md`](research/2026-07-18-traefik-runtime-validation.md) and [`../testdata/README.md`](../testdata/README.md)

## Phase 6: Plugin Hub and Kubernetes release polish

- [x] Align package/API shape, manifest, module, tags, and documentation with the current Traefik plugin development guide.
- [x] Replace the bundled production-style MMDB with the small deterministic test fixture under its selected MIT license.
- [x] Add required MaxMind attribution and database-update guidance.
- [x] Rewrite README installation/configuration for public versioned plugins and Kubernetes CRDs while retaining a concise Swarm example.
- [x] Add complete Kubernetes Helm values and Middleware examples, including PVC path and trusted node CIDR.
- [x] Correct aliases and ensure every example matches actual fields/defaults.
- [x] Add changelog, migration guide, compatibility matrix, security policy, and release checklist.
- [ ] Publish a versioned release only after Hub analysis succeeds.

Exit condition: a new operator can install and configure the plugin on Traefik Kubernetes without relying on repository knowledge.

Release readiness: [`research/2026-07-18-release-readiness.md`](research/2026-07-18-release-readiness.md)

## Phase 7: Authorized cluster canary and rollout

- [ ] Revalidate live Traefik version, node CIDRs, PVC checksum, entry points, and route topology.
- [ ] Enable the versioned plugin in Helm configuration.
- [ ] Create a canary Middleware and attach it to a controlled router.
- [ ] Test Cloudflare IPv4/IPv6, direct IPv4, each fallback header, malformed/spoofed headers, whitelists, block decisions, and database replacement.
- [ ] Observe latency, memory per replica, error logs, and false-positive behavior.
- [ ] Define rollback and then roll out incrementally.

This phase requires explicit write authorization for the cluster and its configuration repository.
