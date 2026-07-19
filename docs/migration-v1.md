# Migration from v1

The v2 line contains major behavior changes. Do not point production Traefik
at a branch; migrate a canary to a tested immutable prerelease or final tag.

## Changes after `v2.0.0-alpha`

The beta candidate hardens several alpha defaults:

- `clientIPHeaders` defaults to only `X-Forwarded-For`; provider headers are
  opt-in per route.
- `rejectInvalidClientIPHeaders` defaults to `true`, so a malformed present
  header from a trusted peer invokes `invalidClientIPPolicy` instead of
  silently becoming the proxy address.
- `invalidClientIPPolicy` and `privateIPPolicy` default to `deny`.
- `logLevel` defaults to `info`, and deny decisions are emitted at info.
- database replacement I/O no longer queues concurrent requests.

Review these fields explicitly when moving an alpha configuration to beta.

## 1. Configure trusted peers

v1 trusted `CF-Connecting-IP` and the first XFF value from every request. The
new resolver ignores all client-IP headers until the immediate socket peer is
in `trustedProxyCIDRs`.

```yaml
trustedProxyCIDRs:
  - 10.17.1.0/24
clientIPHeaders:
  - X-Forwarded-For
rejectInvalidClientIPHeaders: true
```

The audited Kubernetes cluster needs the node CIDR because
`externalTrafficPolicy: Cluster` replaces the external peer with a node/SNAT
address. Configure Traefik's entry-point `forwardedHeaders.trustedIPs` as well.

Provider-specific headers remain supported, but enable them only on a route
whose upstream guarantees that direct clients cannot preserve or inject them.
Traefik rewrites `X-Real-IP` to the immediate peer, so it is not a default
source.

## 2. Move prefix bypasses

v1 treated every `whitelistedPaths` item as a raw string prefix. The new field
is exact and normalized:

```yaml
whitelistedPaths:
  - /health
whitelistedPathPrefixes:
  - /.well-known
  - /api/public
```

Remove trailing slashes from exact paths. Prefix rules are segment-safe, so
`/api/public` does not match `/api/publicity`.

## 3. Select explicit failure policies

`failOpen` remains only as a compatibility bridge when
`databaseFailurePolicy` is `legacy`. New configurations should set every
policy deliberately:

```yaml
databaseFailurePolicy: deny
lookupFailurePolicy: deny
invalidClientIPPolicy: deny
unknownCountryPolicy: deny
unknownSubdivisionPolicy: deny
privateIPPolicy: deny
rejectInvalidClientIPHeaders: true
```

The example is strict. Use `allow` where application availability is more
important than enforcement availability. `databaseFailurePolicy: error`
rejects Middleware construction instead of serving a 403.

## 4. Validate all lists before rollout

v1 logged and skipped malformed whitelist entries. The new version rejects the
entire Middleware configuration for malformed:

- blocked state codes;
- exact IPs and CIDRs;
- trusted proxy IPs and CIDRs;
- header names;
- exact and prefix paths;
- policies, durations, cache bounds, and templates.

Run manifests through a canary Traefik instance and inspect configuration
errors before attaching the Middleware to production routers.

## 5. Mount the production database

The repository no longer contains a production-style
`data/GeoLite2-City.mmdb`. Mount a current City database into every Traefik pod
and use its container path:

```yaml
dbPath: /data/geolite/GeoLite2-City.mmdb
databaseReloadInterval: 1m
```

The reader is shared by all Middleware resources using the same normalized
path. Atomic replacement is detected without a restart, and a failed reload
keeps the last known-good reader.

## 6. Review private traffic and whitelists

The default denies private, loopback, link-local, and unspecified clients so a
missing client header cannot turn a node-SNAT address into an implicit bypass.
Explicit `whitelistedIPs`, `whitelistedPaths`, and
`whitelistedPathPrefixes` still take priority.

## 7. Review templates and logs

- Inline and file templates are mutually exclusive and limited to 1 MiB.
- Templates use `html/template`; `{{STATE}}` remains supported and escaped.
- Deny decisions are logged at `info`; routine allows/cache hits/lookups remain
  at `debug`.
- `logClientIP` is false by default and should be enabled only with an
  appropriate retention and access policy.

## Suggested canary order

1. Publish and pin the new major tag in non-production Traefik.
2. Verify MMDB mount and plugin startup on every replica.
3. Attach a strict Middleware to one controlled route.
4. Test Cloudflare IPv4 and IPv6, XFF fallback, malformed headers, a blocked
   state, non-US traffic, missing headers, and all bypasses.
5. Observe status codes, memory, latency, warnings, and false positives.
6. Expand route coverage incrementally with a documented rollback tag.
