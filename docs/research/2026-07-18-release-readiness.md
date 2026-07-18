# Traefik Plugin Catalog release readiness

Date: 2026-07-18

The current GitHub and catalog state was compared with Traefik's current plugin
development guide.

## Requirements already satisfied

- The repository is public on GitHub and does not present as a fork.
- The `traefik-plugin` topic is present, together with `traefik` and `geoip`.
- The root module name and manifest import both use
  `github.com/vikewoods/traefik-plugin-state-geo`.
- Existing semantic tags are present (`v1.0.0` through `v1.1.2` locally).
- Dependencies are committed under `vendor/`.
- The root package exposes the required `Config`, `CreateConfig`, and `New`
  API and has the module-path-compatible underscore package name.
- `.traefik.yml` has all required fields, explicitly selects Yaegi, and now
  supplies meaningful test data that opens the deterministic City MMDB.
- `scripts/verify-manifest.sh` decodes that test data over `CreateConfig`
  defaults and constructs the Middleware successfully.
- Traefik v3.7.1 loads the plugin through its local interpreted-plugin path and
  passes the HTTP decision matrix.

## Release gates still requiring owner/release action

- Select a project license. The public repository currently exposes no root
  project license; third-party licenses are preserved independently.
- Review and commit the working tree.
- Choose the next major version because trusted-header and path behavior are
  breaking changes.
- Update the changelog version/date and create an immutable semantic tag.
- Push the commit/tag and allow the Traefik Catalog analyzer to inspect that
  exact archive.
- Resolve any analyzer-created GitHub issue with a new tag rather than moving
  the published tag.
- Run an authorized Kubernetes canary before general rollout.

The code and repository preparation do not authorize any of those external
write operations.
