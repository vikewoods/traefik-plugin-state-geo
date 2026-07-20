# Compatibility

| Plugin line | Traefik runtime | Go for native checks | Status |
| --- | --- | --- | --- |
| v2.0.0-rc.1 | v3.7.1 and v3.7.6 / Yaegi | 1.23 minimum; current stable in CI | Release candidate; native and interpreted validation required before tagging |
| v2.0.0-beta | v3.7.1 / Yaegi | 1.23 | Catalog indexed, but not downloadable because its Go module path omitted `/v2` |
| v2.0.0-alpha | v3.7.1 / Yaegi | 1.23 | Published prerelease; superseded by the hardened release candidate |
| v1.1.2 | Traefik v3-era local plugin deployment | 1.23 | Published legacy behavior; Kubernetes hardening is not present |

The v2 line is tested against Traefik v3.7.1 from the audited VS cluster and
v3.7.6 from the FE production cluster. Other Traefik minor releases may use a
different Yaegi version and require the same smoke test before being declared
supported.

The v2 release line must be configured with module name
`github.com/vikewoods/traefik-plugin-state-geo/v2`. The repository URL remains
unchanged; `/v2` is the Go semantic-import-version suffix. The plugin manifest
sets `basePkg: traefik_plugin_state_geo` because Traefik otherwise derives the
Yaegi package name from the final `v2` path segment.

## MMDB data compatibility

The plugin consumes the runtime City field schema rather than requiring a
particular MMDB metadata database name. Records must provide
`country.iso_code`; `subdivisions[].iso_code` is optional, and only the first
subdivision is used. Arbitrary MMDB record schemas are not supported.

The supported and tested inputs are:

- MaxMind GeoLite2-City;
- MaxMind GeoIP2-City;
- stategeodb compliance artifacts whose database type is
  `StateGeo-Country-USSubdivision`, with country coverage globally and an
  optional first subdivision for US records.

The tested compact artifact uses 24-bit records and metadata IP version 6,
which holds both IPv4 and IPv6 networks. Its native reader behavior and Yaegi
plugin behavior passed the compatibility matrix with Traefik v3.7.1 and
v3.7.6. This compact format is an optional compatible input, not a plugin
dependency and not a replacement requirement for GeoLite2-City or GeoIP2-City.

An MMDB with metadata IP version 6 can contain both IPv4 and IPv6 networks; the
audited GeoLite2 production database and the tested compact compliance artifact
both have that form.

The Kubernetes examples use `traefik.io/v1alpha1` Middleware and IngressRoute
resources. The Traefik Kubernetes CRD provider must be enabled, and
cross-namespace Middleware references require `allowCrossNamespace: true`.
