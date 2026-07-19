# Decision and failure policies

The middleware exposes separate policies for conditions that do not produce a normal country/subdivision decision. Policies are validated when the Middleware is constructed; an unknown value is a configuration error.

## Decision table

| Configuration | Condition | Values | Default | `deny` result |
| --- | --- | --- | --- | --- |
| `databaseFailurePolicy` | `dbPath` is empty or the MMDB cannot be opened | `allow`, `deny`, `error`, `legacy` | `legacy` | Middleware starts and returns 403 after path/IP whitelist checks |
| `lookupFailurePolicy` | The loaded MMDB returns a lookup error | `allow`, `deny` | `allow` | 403; the failed result is not cached |
| `invalidClientIPPolicy` | `RemoteAddr` is invalid, or a present trusted header is invalid in strict mode | `allow`, `deny` | `deny` | 403 after the path whitelist check |
| `unknownCountryPolicy` | Lookup succeeds but returns no country | `allow`, `deny` | `allow` | 403 with `Unknown` as the template state |
| `unknownSubdivisionPolicy` | Country is US, state blocking is enabled, and no subdivision is returned | `allow`, `deny` | `deny` | 403 with `Unknown` as the template state |
| `privateIPPolicy` | Resolved client is private, loopback, link-local, or unspecified | `allow`, `lookup`, `deny` | `deny` | 403 without a GeoIP lookup |

`privateIPPolicy: lookup` sends these non-public addresses through the normal
MMDB lookup path. Because public GeoLite databases generally have no result
for them, the result will usually be governed by `unknownCountryPolicy`.

## Database startup behavior

`databaseFailurePolicy` has one additional value because the failure occurs during Middleware construction:

- `allow`: construct the Middleware without a database and allow non-whitelisted requests.
- `deny`: construct the Middleware without a database and deny non-whitelisted requests.
- `error`: return a construction error. Traefik then applies its plugin/Middleware error behavior.
- `legacy`: use the deprecated `failOpen` field. `failOpen: true` maps to `allow`; `failOpen: false` maps to `error`.

`failOpen` remains only as a migration bridge for published configurations. New configurations should select `databaseFailurePolicy` directly.

## Evaluation order

The order is intentional:

1. An exact or segment-safe prefix path whitelist can bypass every client-IP and database decision. This supports narrowly scoped health and ACME endpoints.
2. Client IP is resolved. `invalidClientIPPolicy` applies if `RemoteAddr` is
   unusable or strict trusted-header parsing fails.
3. A valid client IP or CIDR whitelist bypasses database availability and geography decisions.
4. `databaseFailurePolicy` applies if no runtime database exists.
5. A cached normal geography decision is used when present.
6. `privateIPPolicy` applies.
7. GeoIP lookup runs; lookup, unknown-country, unknown-subdivision, country, and state policy then apply.

Lookup errors are not cached so a transient reader failure can recover. Normal
unknown-country/subdivision decisions use the bounded TTL/LRU cache, which is
cleared whenever the database generation changes.

## Recommended strict K8s posture

For the audited K8s ingress path, a strict configuration should not silently allow the SNATed node address when all expected client headers are missing or malformed:

```yaml
databaseFailurePolicy: deny
lookupFailurePolicy: deny
invalidClientIPPolicy: deny
unknownCountryPolicy: deny
unknownSubdivisionPolicy: deny
privateIPPolicy: deny
rejectInvalidClientIPHeaders: true
```

This posture favors enforcement availability over application availability. A less strict service can independently choose `allow` for lookup/unknown cases without changing header trust or database startup behavior.

Whitelisted health paths remain available when the database is absent under `databaseFailurePolicy: deny`. `databaseFailurePolicy: error` is stronger at configuration time but can affect router availability through Traefik's own plugin failure handling, so it should be tested during the canary phase before production use.
