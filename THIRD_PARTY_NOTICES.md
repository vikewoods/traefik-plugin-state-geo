# Third-party notices

## MaxMind DB test data

`testdata/GeoIP2-City-Test.mmdb` comes from the
[MaxMind-DB repository](https://github.com/maxmind/MaxMind-DB).

Copyright (c) 2013-2026 MaxMind, Inc. The fixture is redistributed under the
MIT option supplied by MaxMind. The license text is in
[`testdata/LICENSE-MIT`](testdata/LICENSE-MIT).

This fixture is synthetic test data and is not suitable for production
geolocation. Production GeoLite2 data is not distributed by this repository
and remains subject to MaxMind's database license and attribution terms.

## Vendored Go dependencies

Traefik's interpreted Go plugin runtime requires dependencies to be committed
under `vendor/`.

- `github.com/oschwald/maxminddb-golang` v1.13.1 — ISC license, stored at
  `vendor/github.com/oschwald/maxminddb-golang/LICENSE`.
- `golang.org/x/sys` v0.26.0 — BSD-3-Clause license, stored at
  `vendor/golang.org/x/sys/LICENSE`.

The vendored MaxMind reader contains the repository's documented pure-Go
compatibility shim for Traefik/Yaegi. Regenerating `vendor/` without restoring
and revalidating that shim will break the interpreted-plugin runtime.
