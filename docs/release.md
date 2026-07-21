# Release checklist

Use this checklist for every v1.2 compatibility prerelease and final release.
Do not publish or deploy a new tag until every required item below passes.

## Repository and legal

- [x] The project uses Apache License 2.0. Third-party fixture and vendored
      dependency licenses are preserved independently.
- [ ] `CHANGELOG.md` moves Unreleased changes under the final version/date.
- [ ] README and examples reference the final immutable tag where appropriate.
- [ ] No production MMDB, credentials, logs, or cluster secrets are present in
      the release archive.
- [ ] `THIRD_PARTY_NOTICES.md` and `testdata/LICENSE-MIT` are included.

## Traefik Plugin Catalog requirements

- [ ] The public GitHub repository is not a fork and has the `traefik-plugin`
      topic.
- [ ] `.traefik.yml` is at repository root and its `import` exactly matches
      `go.mod` (`github.com/vikewoods/traefik-plugin-state-geo`).
- [ ] `.traefik.yml` `testData` constructs the Middleware and opens the tiny
      deterministic database.
- [ ] `.traefik.yml` `basePkg` matches the root Go package.
- [ ] Dependencies are committed under `vendor/`.
- [ ] The package exports `Config`, `CreateConfig`, and `New` with the required
      Traefik signatures.
- [ ] The release tag uses semantic `vMAJOR.MINOR.PATCH` or
      `vMAJOR.MINOR.PATCH-PRERELEASE` form.
- [ ] The v1 release uses the repository-root module path without a major
      suffix.

## Verification

```bash
gofmt -w .
go mod tidy
git diff --exit-code -- go.mod go.sum
go test -shuffle=on -cover ./...
go test -race ./...
go vet ./...
golangci-lint run ./...
govulncheck ./...
gosec -quiet ./...
shellcheck scripts/*.sh
./scripts/verify-manifest.sh
./scripts/verify-vendor-shim.sh
./scripts/traefik-smoke-test.sh
git diff --check
```

- [ ] All commands pass on the release commit.
- [ ] The smoke test uses the same Traefik minor version as production.
- [ ] The Kubernetes manifests parse and the canary plan has an approved
      rollback tag.
- [ ] Benchmarks are compared with the Phase 3 baseline when hot-path code
      changes.

## Publish and observe

1. Create and push the immutable annotated tag.
2. Confirm the tag is available through the Go module proxy under
   `github.com/vikewoods/traefik-plugin-state-geo`.
3. Confirm the exact tag returns HTTP 200 from Traefik's public download and
   validation endpoints before changing a cluster.
4. Wait for the Traefik Plugin Catalog polling cycle.
5. Check for an analyzer-created GitHub issue; fix the release with a new tag,
   never by moving an existing tag.
6. Confirm the catalog page shows the new version and current README.
7. Enable the versioned tag on a controlled Traefik/Kubernetes canary.
8. Complete the matrix in `docs/kubernetes.md`, observe memory/latency/errors,
   then roll out incrementally.
