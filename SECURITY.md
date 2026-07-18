# Security policy

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting or a private repository
Security Advisory. Do not open a public issue containing exploit details,
sensitive headers, real client addresses, MaxMind credentials, or cluster
configuration secrets.

Include the affected plugin tag and Traefik version, deployment provider,
reproduction steps, expected and actual behavior, and whether the issue is
reachable from an untrusted request.

## Supported versions

Security fixes are prepared for the latest released major line. Older tags may
receive a fix when the issue is severe and a safe backport is practical. The
current matrix is maintained in [`docs/compatibility.md`](docs/compatibility.md).

## Deployment responsibility

This middleware makes decisions from request metadata supplied across proxy
trust boundaries. Operators must configure `trustedProxyCIDRs`, Traefik
entry-point `forwardedHeaders.trustedIPs`, route topology, upstream header
sanitization, and failure policies correctly. The plugin cannot recover source
authenticity already removed by Kubernetes SNAT or an untrusted upstream.
