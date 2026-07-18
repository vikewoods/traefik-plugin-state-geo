# State Geo Block Middleware

State Geo Block is a Traefik HTTP middleware that blocks non-US traffic and
selected US subdivisions using a mounted MaxMind GeoLite2 or GeoIP2 City
database. It supports trusted Cloudflare and proxy headers, direct connections,
IPv4 and IPv6, explicit failure policies, database hot reload, and custom block
pages.

[![CI](https://github.com/vikewoods/traefik-plugin-state-geo/actions/workflows/ci.yml/badge.svg)](https://github.com/vikewoods/traefik-plugin-state-geo/actions/workflows/ci.yml)
[![CodeQL](https://github.com/vikewoods/traefik-plugin-state-geo/actions/workflows/codeql.yml/badge.svg)](https://github.com/vikewoods/traefik-plugin-state-geo/actions/workflows/codeql.yml)

## What it does

- resolves clients from trusted `CF-Connecting-IP`, `True-Client-IP`,
  `X-Forwarded-For`, RFC `Forwarded`, `X-Real-IP`, custom headers, or the
  direct socket peer;
- parses IPv4, IPv6, mapped IPv4, bracketed IPv6, and forwarding chains;
- blocks every non-US country when `blockNonUS` is enabled;
- blocks configured two-letter US subdivision codes when `blockUSStates` is
  enabled;
- supports exact IPs, CIDRs, exact path bypasses, and segment-safe path-prefix
  bypasses;
- shares one in-memory MMDB reader per database path and reloads atomic file
  replacements without restarting Traefik;
- provides bounded TTL/LRU decisions, escaped HTML templates, and structured
  privacy-conscious logs;
- gives database, lookup, invalid-IP, unknown-country, unknown-subdivision, and
  private-IP cases independent policies.

## Trust boundary

Forwarding headers are ignored unless the immediate `RemoteAddr` peer matches
`trustedProxyCIDRs`. This prevents an ordinary direct client from selecting its
own geography with a forged header.

Traefik also has a separate entry-point setting,
`forwardedHeaders.trustedIPs`, which controls whether it preserves incoming
`X-Forwarded-*` values. Configure both layers. With Kubernetes
`externalTrafficPolicy: Cluster`, the plugin usually sees a node-SNAT address,
so the node CIDR must be trusted and upstream header sanitization remains part
of the security boundary.

The safe default config trusts no proxy. See
[Client IP resolution](docs/client-ip-resolution.md) for the full algorithm and
the Traefik-specific `X-Real-IP` behavior.

## Install a versioned plugin

State Geo Block is listed in the
[Traefik Plugin Catalog](https://plugins.traefik.io/plugins/69b668184cda2b265225fa62/state-geo-block-middleware).
Configure a released tag in Traefik's static configuration; do not use a branch
name in production.

```yaml
experimental:
  plugins:
    stateGeoBlock:
      moduleName: github.com/vikewoods/traefik-plugin-state-geo
      version: vX.Y.Z
```

The alias `stateGeoBlock` is operator-selected but must be used consistently in
the dynamic Middleware configuration.

## Kubernetes quick start

Mount a current City MMDB into every Traefik pod, configure Traefik's
entry-point trusted IPs, and then create the Middleware:

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: state-geo-block
  namespace: traefik-system
spec:
  plugin:
    stateGeoBlock:
      dbPath: /data/geolite/GeoLite2-City.mmdb
      databaseReloadInterval: 1m
      trustedProxyCIDRs:
        - 10.17.1.0/24
      clientIPHeaders:
        - CF-Connecting-IP
        - True-Client-IP
        - X-Forwarded-For
        - Forwarded
        - X-Real-IP
      blockNonUS: true
      blockUSStates: true
      blockedStates: [CA, NY]
      databaseFailurePolicy: deny
      lookupFailurePolicy: deny
      invalidClientIPPolicy: deny
      unknownCountryPolicy: deny
      unknownSubdivisionPolicy: deny
      privateIPPolicy: deny
      whitelistedPaths:
        - /health
      whitelistedPathPrefixes:
        - /.well-known
```

`10.17.1.0/24`, `/data/geolite/GeoLite2-City.mmdb`, and the namespace above are
specific to the audited cluster and must be changed for other installations.
The complete Helm/PVC, Middleware, IngressRoute, standard Ingress, update, and
canary instructions are in [Kubernetes deployment](docs/kubernetes.md).

## Configuration

| Field | Default | Description |
| --- | --- | --- |
| `dbPath` | empty | Path inside the Traefik container to a MaxMind City MMDB. |
| `databaseReloadInterval` | `1m` | Minimum interval between request-driven file replacement checks; minimum `1s`. |
| `cacheSize` | `1000` | Per-Middleware LRU entry bound; `0` disables, maximum `100000`. |
| `cacheTTL` | `15m` | Positive decision TTL when caching is enabled. |
| `clientIPHeaders` | CF, True Client, XFF, Forwarded, X-Real | Ordered trusted client-IP sources. |
| `trustedProxyCIDRs` | empty | Immediate peers permitted to supply client-IP headers. |
| `blockNonUS` | `true` | Deny known non-US countries. |
| `blockUSStates` | `true` | Apply `blockedStates` and unknown-subdivision policy to US records. |
| `blockedStates` | empty | Two-letter US subdivision codes to deny, normalized to uppercase. |
| `whitelistedIPs` | empty | Exact IPv4/IPv6 addresses and CIDRs that bypass geography decisions. |
| `whitelistedPaths` | empty | Normalized exact request paths that bypass all other decisions. |
| `whitelistedPathPrefixes` | empty | Normalized, segment-safe path prefixes that bypass all other decisions. |
| `templateHTML` | built-in | Inline escaped HTML template; supports `{{STATE}}` or `{{.State}}`. |
| `templatePath` | empty | Mounted HTML template path; mutually exclusive with `templateHTML`. |
| `databaseFailurePolicy` | `legacy` | `allow`, `deny`, `error`, or deprecated compatibility mode `legacy`. |
| `lookupFailurePolicy` | `allow` | `allow` or `deny` when an MMDB lookup returns an error. |
| `invalidClientIPPolicy` | `allow` | `allow` or `deny` when `RemoteAddr` is unusable. |
| `unknownCountryPolicy` | `allow` | `allow` or `deny` when lookup returns no country. |
| `unknownSubdivisionPolicy` | `deny` | `allow` or `deny` for a US record without a subdivision. |
| `privateIPPolicy` | `allow` | `allow`, `lookup`, or `deny` for private/loopback clients. |
| `logLevel` | `warn` | `off`, `error`, `warn`, `info`, or `debug`. |
| `logClientIP` | `false` | Include resolved client IPs in structured logs. |
| `failOpen` | `true` | Deprecated bridge used only by `databaseFailurePolicy: legacy`. |

Invalid state, IP/CIDR, path, duration, policy, template, and header
configuration fails Middleware construction instead of being silently skipped.
See [Failure policies](docs/failure-policies.md),
[Database lifecycle](docs/database-lifecycle.md), and
[Middleware hardening](docs/middleware-hardening.md) for exact behavior.

## Database requirements

The repository does not ship a production GeoLite2 database. Obtain and update
`GeoLite2-City.mmdb` under the
[MaxMind GeoLite terms](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data),
mount it read-only into Traefik, and replace it atomically. The plugin detects a
size or modification-time change, validates the replacement, retains the last
known-good reader if reload fails, and invalidates cached decisions after a
successful swap.

## Upgrade and development

Published v1 configurations require migration because trusted headers, path
prefixes, invalid rules, failure policies, and bundled data behavior changed.
Read [Migration from v1](docs/migration-v1.md) before upgrading.

```bash
go test ./...
go test -race ./...
go vet ./...
golangci-lint run ./...
govulncheck ./...
gosec -quiet ./...
./scripts/traefik-smoke-test.sh
```

The smoke test loads the source through Traefik v3.7.1's interpreted local
plugin runtime and verifies real IPv4/IPv6 decisions. See
[Contributing](CONTRIBUTING.md) and the [release checklist](docs/release.md).
