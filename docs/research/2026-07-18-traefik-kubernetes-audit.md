# Traefik Kubernetes readiness audit

Date: 2026-07-18  
Repository: `github.com/vikewoods/traefik-plugin-state-geo`  
Cluster repository: `/Users/vikewoods/Documents/Work/homelab/vs-k8s-cluster`  
Read-only Kubernetes context used for the audit: `admin@vs-k8s-01`

## Purpose

This document preserves the repository, Traefik, Kubernetes, traffic, and upstream-documentation research used to prepare the middleware for Kubernetes. It is a point-in-time audit, not deployment instructions.

## Accepted operating decisions

1. MaxMind credentials committed in the private VS repository are an explicitly accepted risk. Credential removal or rotation is out of scope unless that decision changes.
2. Traefik will keep `externalTrafficPolicy: Cluster`. `Local` will not be enabled because concentrating MetalLB L2 traffic on the announcing node is not acceptable for this cluster.
3. The plugin must resolve clients from Cloudflare and other common proxy headers, with a direct socket-address fallback, and must support IPv4 and IPv6.
4. Cluster access during this work remains read-only. A later deployment or canary requires separate authorization.

## Executive findings

The live GeoLite2 PVC is healthy and consistently mounted in all Traefik replicas. The current plugin is not yet configured in the cluster. The largest correctness and security gap in the code is unconditional trust of client-controlled forwarding headers. The largest Kubernetes constraint is that `externalTrafficPolicy: Cluster` hides the original socket peer behind node-level SNAT, so the plugin must trust the node CIDR if it is to read forwarded headers.

Trusting the node CIDR is workable, but it has a hard limit: after SNAT, middleware cannot prove whether a `CF-Connecting-IP` header came from Cloudflare or was supplied by a direct client and preserved upstream. Header authenticity therefore also depends on route isolation and Traefik/header-sanitization policy. This is a network trust-boundary fact, not something the Go resolver can fully solve.

## Repository baseline

Audited revision state:

- Branch: `master`, tracking `origin/master`
- Latest published tag inspected: `v1.1.2`
- Working tree was clean before implementation began
- Go module: `github.com/vikewoods/traefik-plugin-state-geo`
- Declared Go version: 1.23
- GeoIP dependency: `github.com/oschwald/maxminddb-golang v1.13.1`

Baseline verification completed successfully:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `gofmt` check
- `gosec ./...`
- `govulncheck ./...` reported no known reachable vulnerabilities
- Test coverage was approximately 91.7%, although coverage depended partly on the bundled production-style MMDB rather than a deterministic fixture

### Code observations

The baseline middleware:

- trusted `CF-Connecting-IP` unconditionally;
- otherwise selected the first `X-Forwarded-For` value unconditionally;
- otherwise used `RemoteAddr`;
- did not validate the immediate proxy before trusting a header;
- did not support `X-Real-IP`, RFC 7239 `Forwarded`, custom header ordering, or a trusted proxy chain;
- opened the MaxMind database once at middleware construction time;
- inherited the vendored pure-Go MaxMind shim, which reads the complete database into memory for every middleware instance;
- had no database hot reload, so the weekly atomic database replacement was not observed until middleware recreation;
- used a fixed 1,000-entry cache with no TTL or eviction policy;
- automatically allowed private and loopback addresses;
- blocked US records with no subdivision as `Unknown` whenever state blocking was enabled;
- treated path whitelist entries as unrestricted string prefixes;
- logged verbose per-request messages directly to stdout/stderr;
- included a roughly 60 MiB GeoLite2 database in the plugin repository and release archive;
- used a `.traefik.yml` `testData` configuration with no `dbPath`, causing the Hub analyzer path to exercise pass-through rather than real lookup behavior;
- had Swarm-centric and internally inconsistent README examples.

The vendored MaxMind shim is intentional Yaegi compatibility work. Upgrading or restoring memory-mapped implementation files must be tested inside Traefik's plugin interpreter, not only with native `go test`.

## Live cluster evidence

### Traefik runtime

- Deployment health: 3/3 replicas ready
- Traefik image: v3.7.1
- Helm chart: 40.2.0
- Runtime Go version reported by Traefik: Go 1.25.10
- Observed pod memory: approximately 98-105 MiB per replica before enabling this plugin
- No State Geo Block plugin declaration or plugin Middleware resource was present at audit time

### GeoLite2 storage

PVC `traefik-geolite-db` was:

- `Bound`
- Longhorn-backed
- `ReadWriteMany`
- 1 GiB
- mounted read-only at `/data/geolite` in all three Traefik pods

Each Traefik pod saw the same file:

- path: `/data/geolite/GeoLite2-City.mmdb`
- size: 65,864,808 bytes
- SHA-256: `e2765534f9fc6e0bcda4c46d8bc58bfac5feea6ca2d5581219e53c99cd3b073d`
- database type: `GeoLite2-City`
- IP version: 6, meaning the database supports both IPv4 and IPv6 lookups
- database build timestamp: 2026-07-14 05:32:54 UTC

The updater's last observed successful run was 2026-07-15 03:17:15 UTC. It runs weekly on Wednesday at 03:17 and replaces the database atomically through a temporary file and rename. Storage and update mechanics are therefore healthy; plugin-side reload remains necessary.

### Traffic path and IP families

Traefik's Service used:

- `externalTrafficPolicy: Cluster`
- IPv4 single-stack Service networking
- MetalLB L2

The nodes and pods were IPv4-addressed. Traefik access logs showed private `10.x` socket peers because kube-proxy/node SNAT had already replaced the external peer address.

In the reproducible access-log sample from 2026-07-18 20:38:05-20:41:42 UTC:

- total sampled rows: 6,847
- Cloudflare client IPv4 values: 2,517
- Cloudflare client IPv6 values: 4,329
- missing client value: 1
- IPv6 share: approximately 63.2%

IPv6 client-header parsing is consequently a current production requirement even though the Kubernetes Service itself is IPv4 single-stack.

### Existing forwarded-header configuration

The observed trusted proxy configuration included the node network `10.17.1.0/24` and Cloudflare ranges. The `websecure` entry point included Cloudflare IPv4 and IPv6 ranges; `web` omitted the IPv6 ranges. Traefik's `forwardedHeaders.trustedIPs` controls its handling of `X-Forwarded-*`; it does not establish authenticity for arbitrary headers such as `CF-Connecting-IP`.

Cloudflared tunnel routes inspected during the audit targeted backend Services directly rather than Traefik. The plugin can only enforce policy on requests whose route actually passes through a Traefik router with this middleware attached.

## Risk and remediation register

| Priority | Finding | Impact | Planned phase |
| --- | --- | --- | --- |
| Critical | Forwarding headers were trusted from every peer | Direct clients could spoof geography or whitelist membership | Phase 1 |
| High | Cluster SNAT removes the original socket peer | Trusted node CIDR is required; header authenticity also depends on route/header controls | Phase 1 documentation and deployment design |
| High | Private/loopback IPs were always allowed | A missing or unusable header can silently bypass geo policy in Kubernetes | Phase 2 |
| High | Database is loaded per middleware and never reloaded | Memory multiplies with middleware instances; weekly updates remain stale | Phase 3 |
| Medium | Failure behavior is implicit and inconsistent | Operators cannot choose policy for invalid IP, lookup failure, or unknown geography | Phase 2 |
| Medium | US records without subdivision are always blocked | False positives cannot be configured independently | Phase 2 |
| Medium | Cache is fixed-size with no expiry or meaningful eviction | Stale decisions and ineffective caching under high cardinality | Phase 3 |
| Medium | Whitelisted paths use raw prefix matching | `/health-attack` can match `/health`; empty prefix can bypass everything | Phase 4 |
| Medium | Per-request stdout/stderr logging is unconditional | Noise, avoidable allocation, and possible client-IP disclosure | Phase 4 |
| Medium | Hub analyzer configuration exercises no-op behavior | Published compatibility signal is weaker than expected | Phase 5/6 |
| Medium | Large, stale database is shipped in the repository/tag | Large downloads, licensing/attribution burden, and non-reproducible tests | Phase 6 |
| Low | Documentation is Swarm-focused and aliases drift | K8s installation is error-prone | Phase 6 |
| Accepted | MaxMind credentials are committed in the trusted VS repository | Known secret-management risk accepted by repository owner | No action |

## Phase 1 client-IP design decision

The resolver will use two explicit configuration values:

- `clientIPHeaders`: ordered header sources. Known chain headers (`X-Forwarded-For` and `Forwarded`) get chain-aware parsing; any other valid HTTP header name is treated as a single-IP source, which supports provider-specific headers without code changes.
- `trustedProxyCIDRs`: socket peers allowed to supply those headers. With no trusted proxies configured, all headers are ignored and `RemoteAddr` is used.

Default header order is:

1. `CF-Connecting-IP`
2. `True-Client-IP`
3. `X-Forwarded-For`
4. `Forwarded`
5. `X-Real-IP`

The chain headers precede `X-Real-IP` because Traefik normally rewrites
`X-Real-IP` to the immediate socket peer before the plugin runs. Keeping it
ahead of `X-Forwarded-For` would select the Kubernetes node/private peer and
mask the useful forwarding chain.

Resolution rules:

- parse and normalize IPv4 and IPv6, including IPv4-mapped IPv6;
- reject ambiguous single-IP headers;
- scan forwarding chains from the trusted side (right to left), selecting the first untrusted address;
- ignore forwarding headers from untrusted socket peers;
- continue to the next configured source if a present header is malformed;
- fall back to the direct socket peer;
- fail middleware construction on invalid trusted CIDRs or invalid configured header names.

For this cluster, a future Middleware configuration will need `10.17.1.0/24` in `trustedProxyCIDRs` because of `externalTrafficPolicy: Cluster`. That choice must be paired with route-level controls that prevent untrusted direct traffic from injecting a preserved higher-priority provider header.

## Upstream references

- [Traefik plugin development guide](https://doc.traefik.io/traefik-hub/api-gateway/guides/plugin-development-guide)
- [Traefik plugin installation configuration](https://doc.traefik.io/traefik/master/reference/install-configuration/experimental/plugins/)
- [Traefik entry point and forwarded-header configuration](https://doc.traefik.io/traefik/master/reference/install-configuration/entrypoints/)
- [Cloudflare HTTP request headers](https://developers.cloudflare.com/fundamentals/reference/http-headers/)
- [Cloudflare IPv4 ranges](https://www.cloudflare.com/ips-v4/)
- [Cloudflare IPv6 ranges](https://www.cloudflare.com/ips-v6/)
- [RFC 7239: Forwarded HTTP Extension](https://www.rfc-editor.org/rfc/rfc7239)
- [MaxMind GeoLite database documentation](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data)
- [maxminddb-golang releases](https://github.com/oschwald/maxminddb-golang/releases)

## Audit boundaries

All Kubernetes inspection was read-only. No cluster resources, Helm values, secrets, workloads, PVC contents, or routes were changed. Cluster observations are a dated snapshot and should be revalidated before deployment.
