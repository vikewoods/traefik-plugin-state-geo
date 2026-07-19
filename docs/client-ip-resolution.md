# Client IP resolution

Geo decisions and IP whitelists are only as reliable as the address selected for the request. This middleware separates two concerns:

- `clientIPHeaders` defines which header sources are understood and their priority.
- `trustedProxyCIDRs` defines which immediate socket peers may supply those headers.

## Secure default

`CreateConfig` enables only `X-Forwarded-For`. Cloudflare headers, RFC 7239
`Forwarded`, `X-Real-IP`, and custom single-IP headers remain supported but are
opt-in because an ordinary load balancer may pass an attacker-supplied
provider header unchanged.

`trustedProxyCIDRs` is empty by default. Consequently, a default configuration
ignores XFF and uses the socket peer from `RemoteAddr`. Present-but-invalid
headers from a trusted peer are rejected by default and are handled by
`invalidClientIPPolicy`, whose default is `deny`.

Any syntactically valid custom header can be placed in `clientIPHeaders`. Custom headers are treated as single-IP values. For example, a deployment could select `Fly-Client-IP` without requiring a plugin code change.

## Kubernetes configuration for this cluster

The audited cluster keeps `externalTrafficPolicy: Cluster`. Its requests reach Traefik with a node-network socket peer due to SNAT, so the node CIDR must be trusted before the plugin can use Cloudflare or forwarded client headers.

Illustrative Middleware configuration, where `stateGeoBlock` is the alias selected in Traefik's static plugin configuration:

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
      trustedProxyCIDRs:
        - 10.17.1.0/24
      clientIPHeaders:
        - X-Forwarded-For
      rejectInvalidClientIPHeaders: true
```

The node CIDR is cluster-specific and must be revalidated before deployment. Do not copy it into another cluster without checking that cluster's real socket peers.

## Selection algorithm

1. Parse and normalize `RemoteAddr` as IPv4 or IPv6.
2. If the immediate peer is not in `trustedProxyCIDRs`, ignore all headers.
3. If the peer is trusted, inspect `clientIPHeaders` in configured order.
4. For a single-IP header, require exactly one unambiguous, valid address.
5. For `X-Forwarded-For` and `Forwarded`, parse the complete chain and walk it from right to left. Select the first address outside the trusted proxy ranges. If every chain entry is trusted, use the leftmost entry.
6. If a present source is malformed, emit a warning containing the header name
   but not its value. With `rejectInvalidClientIPHeaders: true`, stop and apply
   `invalidClientIPPolicy`; with `false`, try the next configured source.
7. If every configured source is absent, use the immediate socket peer.

IPv4-mapped IPv6 addresses are normalized to IPv4. Bracketed IPv6 and common address-with-port forms are supported. Scoped IPv6 zone identifiers are rejected because they are not meaningful as an Internet client identity.

## Trust limitation with Cluster traffic policy

With `externalTrafficPolicy: Cluster`, Kubernetes removes information before the request reaches this middleware. Once a direct request and a Cloudflare request both appear to originate from a trusted node IP, the plugin cannot independently prove which path produced a provider-specific header.

The resolver prevents header spoofing from an untrusted socket peer, but the deployment must also ensure at least one of the following:

- direct traffic cannot reach a route that preserves a client-supplied provider header;
- an upstream trusted component removes and regenerates the selected header;
- distinct entry points or routers use source restrictions appropriate to their ingress path.

Do not add `CF-Connecting-IP` or `True-Client-IP` to a shared Middleware merely
because some routes use Cloudflare. Provider headers are safe only on a route
where the upstream guarantees that direct clients cannot preserve or inject
them. Cloudflare documents `True-Client-IP` as an Enterprise-only equivalent
of `CF-Connecting-IP`; it should not be enabled unless that transform is
actually configured.

## Operational examples

Cloudflare-only route:

```yaml
clientIPHeaders:
  - CF-Connecting-IP
  - X-Forwarded-For
rejectInvalidClientIPHeaders: true
trustedProxyCIDRs:
  - 10.17.1.0/24
```

Generic reverse proxy route:

```yaml
clientIPHeaders:
  - X-Forwarded-For
rejectInvalidClientIPHeaders: true
trustedProxyCIDRs:
  - 10.17.1.0/24
```

Direct traffic with no trusted proxy:

```yaml
clientIPHeaders: []
trustedProxyCIDRs: []
rejectInvalidClientIPHeaders: true
```

The explicit empty lists force `RemoteAddr` and are useful for a route where
the original socket peer is preserved. Traefik normally rewrites `X-Real-IP`
to its immediate peer before middleware execution, so only opt into that
header when another verified component deliberately sets it to the client.
