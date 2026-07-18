# Middleware hardening and logging

## Path whitelist rules

Path rules now have separate, explicit semantics:

- `whitelistedPaths` contains exact paths.
- `whitelistedPathPrefixes` contains segment-safe path prefixes.

```yaml
whitelistedPaths:
  - /health
whitelistedPathPrefixes:
  - /.well-known
  - /api/public
```

In this example:

- `/health` is allowed;
- `/healthz` and `/health/ready` are not matched by the exact rule;
- `/.well-known` and `/.well-known/acme-challenge/token` are allowed;
- `/.well-known-attack` is not matched;
- `/api/public/status` is allowed;
- `/api/publicity` is not matched.

The request path is cleaned before matching. A request such as `/.well-known/../admin` is evaluated as `/admin`, so a downstream path normalization cannot convert an already-whitelisted request into an administrative path.

Configuration validation rejects:

- empty or relative paths;
- query strings and fragments;
- control characters;
- non-normalized paths with traversal or duplicate separators;
- exact paths ending in `/`;
- a `/` prefix, which would whitelist every request.

An exact `/` remains valid and matches only the normalized root path. Prefix entries may be written with or without one trailing slash; they are stored without it.

This changes the published `whitelistedPaths` behavior from raw prefix matching to exact matching. Existing prefix rules must move to `whitelistedPathPrefixes` during upgrade.

## IP and state rule validation

Invalid `whitelistedIPs` entries now fail Middleware construction rather than being logged and skipped. Exact addresses and CIDRs are normalized across IPv4, IPv6, and IPv4-mapped IPv6, then deduplicated.

`blockedStates` entries are trimmed, uppercased, deduplicated, and required to be two ASCII letters. Unknown US subdivision behavior belongs to `unknownSubdivisionPolicy`; `Unknown` should not be placed in `blockedStates`.

## Block-page templates

Block pages are parsed with Go's `html/template`, and the dynamic state value is contextually HTML-escaped. The published `{{STATE}}` placeholder remains supported and is translated to `{{.State}}`; new templates may use either form.

```yaml
templateHTML: |
  <!doctype html>
  <html>
    <body>
      <h1>Access denied</h1>
      <p>State: {{STATE}}</p>
    </body>
  </html>
```

Template rules:

- `templateHTML` and `templatePath` are mutually exclusive.
- Inline and file templates are limited to 1 MiB.
- A missing/unreadable file, invalid template syntax, or oversized template fails Middleware construction.
- The file path remains operator-controlled so it can reference the mounted K8s volume; the plugin does not accept template paths from HTTP requests.
- Rendering occurs into a buffer before headers are written.
- An execution-time custom-template error uses the built-in escaped denial page and emits an error log.

## Structured logging

The plugin no longer prints a line for every request. It uses a private `log/slog` JSON logger and does not modify Traefik's global logger.

```yaml
logLevel: warn
logClientIP: false
```

Supported levels are `off`, `error`, `warn`, `info`, and `debug`. The default `warn` level emits database availability/reload warnings and handled errors, while routine allow/deny/cache/lookup events remain debug-only.

Client addresses are omitted by default, including from invalid-address and lookup-error details that could echo the address. `logClientIP: true` adds a structured `client_ip` attribute and permits detailed lookup/resolution error fields. This is an explicit privacy choice and should only be enabled with an appropriate log retention/access policy.

Log messages are fixed, low-cardinality strings. State, country, path, policy, and errors are structured attributes rather than interpolated message text.

Traefik's existing access logs and metrics remain the source for request rate, latency, and status-code observability. Adding a plugin-owned Prometheus registry inside the shared Traefik process would create registration and lifecycle risks, so no separate metrics dependency is introduced here.
