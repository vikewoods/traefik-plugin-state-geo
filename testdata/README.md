# GeoIP test fixture

`GeoIP2-City-Test.mmdb` is the small deterministic city database from the
[MaxMind-DB repository](https://github.com/maxmind/MaxMind-DB/blob/main/test-data/GeoIP2-City-Test.mmdb).
It is used only for automated tests and examples; it is not suitable for
production geolocation.

- Retrieved: 2026-07-18
- SHA-256: `ed972738e4e03a3e56e12041a6af4d91592249d110f7e4a647e5f2fa0e639c09`
- Copyright: MaxMind, Inc.
- License selected for redistribution here: MIT

The complete selected license is stored in [`LICENSE-MIT`](LICENSE-MIT). The
upstream repository also offers the test data under Apache-2.0.

Stable records exercised by this project include:

| Address | Country | Subdivision |
| --- | --- | --- |
| `214.78.120.1` | US | CA |
| `216.160.83.56` | US | WA |
| `149.101.100.1` | US | unknown |
| `81.2.69.160` | GB | ENG |
| `89.160.20.128` | SE | E |
| `2001:218::1` | JP | unknown |
