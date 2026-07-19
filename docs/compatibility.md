# Compatibility

| Plugin line | Traefik runtime | Go for native checks | Status |
| --- | --- | --- | --- |
| Unreleased v2 beta candidate | v3.7.1 / Yaegi | 1.23 minimum; current stable in CI | Native and interpreted validation required before beta tag |
| v2.0.0-alpha | v3.7.1 / Yaegi | 1.23 | Published alpha; beta candidate changes defaults and reload concurrency |
| v1.1.2 | Traefik v3-era local plugin deployment | 1.23 | Published legacy behavior; Kubernetes hardening is not present |

The v2 line is tested against Traefik v3.7.1 because that exact version runs in
the audited cluster. Other Traefik minor releases may use a different Yaegi
version and require the same smoke test before being declared supported.

The MMDB must be a MaxMind City database with `country.iso_code` and optional
`subdivisions[].iso_code` fields. GeoLite2-City and GeoIP2-City are the intended
formats. A database with metadata IP version 6 can contain both IPv4 and IPv6
networks; the audited production database has that form.

The Kubernetes examples use `traefik.io/v1alpha1` Middleware and IngressRoute
resources. The Traefik Kubernetes CRD provider must be enabled, and
cross-namespace Middleware references require `allowCrossNamespace: true`.
