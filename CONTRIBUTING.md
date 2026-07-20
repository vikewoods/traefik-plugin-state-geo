# Contributing

## Local checks

Use Go 1.23 or newer. Before opening a pull request, run:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
golangci-lint run ./...
govulncheck ./...
gosec -quiet ./...
shellcheck scripts/*.sh
./scripts/verify-manifest.sh
./scripts/traefik-smoke-test.sh
```

The last command requires Docker and defaults to Traefik v3.7.6. CI validates
both v3.7.1 and v3.7.6 interpreted local-plugin runtimes. A native Go build
alone is not a sufficient compatibility check. Set `TRAEFIK_IMAGE` to exercise
another reviewed image.

## Tests

- Keep pure decision and parser behavior table-driven and deterministic.
- Use `testdata/GeoIP2-City-Test.mmdb` for real reader behavior. Do not commit a
  production GeoLite2 database.
- Add IPv4 and IPv6 cases for client-address changes.
- Add a regression test before fixing a reported bug.
- Run the race detector for shared database/cache changes.
- Extend fuzz seeds when a parser edge case is found.

## Vendored reader compatibility

Traefik interprets committed dependencies from `vendor/`. The MaxMind reader
is intentionally adapted to use a pure in-memory `os.ReadFile` path because
Yaegi cannot use the upstream mmap/syscall implementation safely.

Do not run `go mod vendor` and commit the result without restoring the
pure-reader shim, removing the mmap implementation files, and passing
`./scripts/traefik-smoke-test.sh`. The release checklist verifies the expected
shim shape.

## Pull requests

- Unless explicitly stated otherwise, submitted contributions are licensed
  under the project's [Apache License 2.0](LICENSE).
- Keep changes scoped and document behavior changes in `CHANGELOG.md`.
- Update README/configuration examples whenever fields or defaults change.
- Preserve the module path and `.traefik.yml` import consistency.
- Avoid logging client identities by default.
- Do not weaken header trust or strict examples to work around a deployment
  topology problem; document the required proxy/entry-point configuration.
