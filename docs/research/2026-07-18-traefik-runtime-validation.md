# Traefik runtime validation

Date: 2026-07-18

The plugin was loaded as a local plugin by the exact cluster Traefik image,
`traefik:v3.7.1`, rather than only compiled natively with `go test`.

## Findings fixed by the runtime test

1. Yaegi in Traefik v3.7.1 did not support the Go `clear` built-in used in the
   cache invalidation path. Traefik reported `undefined: clear` and disabled
   plugins. Cache reset now replaces the bounded map with a new map, which is
   accepted by Yaegi and has equivalent behavior on database-version changes.
2. Traefik rewrites `X-Real-IP` to the immediate socket peer before plugin
   execution. Placing it ahead of `X-Forwarded-For` therefore selected the
   Docker/Kubernetes private peer and masked the useful forwarding chain.
   Default priority is now provider-specific headers, chain-aware headers, and
   finally `X-Real-IP`.
3. Traefik only preserves incoming `X-Forwarded-For` values when its entry
   point trusts the immediate peer. This matches the live cluster, where the
   node CIDR `10.17.1.0/24` is in `forwardedHeaders.trustedIPs`. Plugin-side
   `trustedProxyCIDRs` is a separate control and both must be configured.

## Verified requests

The disposable end-to-end test runs Traefik, the interpreted plugin, the
official MaxMind city fixture, and a `traefik/whoami` backend. It verifies:

- Cloudflare-header IPv4 allow and deny decisions;
- Cloudflare-header IPv6 deny decisions;
- `True-Client-IP` allow decisions;
- right-to-left `X-Forwarded-For` chain selection;
- fallback from a malformed preferred header to a valid forwarding chain;
- strict denial when Traefik has no usable public client identity.

The reusable command is:

```bash
./scripts/traefik-smoke-test.sh
```

The script uses `forwardedHeaders.insecure=true` only inside its isolated
Docker network. Production must use explicit entry-point trusted ranges, as
the audited Kubernetes cluster already does.

## `X-Real-IP` and `Forwarded` limitation

The parser supports both sources and operators can place either earlier in
`clientIPHeaders`. In a normal Traefik HTTP path, however, Traefik injects an
`X-Forwarded-For` value and rewrites `X-Real-IP` before middleware execution.
Consequently, `X-Real-IP` cannot preserve a client-supplied upstream identity
in this runtime. Prefer `CF-Connecting-IP`, `True-Client-IP`, a trusted
`X-Forwarded-For` chain, or a custom provider header. Put RFC `Forwarded`
before `X-Forwarded-For` when it is the authoritative source for a route.
