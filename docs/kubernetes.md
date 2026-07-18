# Kubernetes deployment

This guide covers a versioned public plugin on Traefik Kubernetes. The concrete
values for `traefik-system`, `10.17.1.0/24`, and `/data/geolite` match the
cluster audited on 2026-07-18. Revalidate them before rollout and replace them
for any other cluster.

No cluster changes were made during repository preparation. Follow the canary
sequence only after approving write access to the cluster repository and live
cluster.

## Prerequisites

- Traefik v3 with the Kubernetes CRD provider enabled.
- A released plugin tag, not a branch or uncommitted revision.
- A MaxMind GeoLite2-City or GeoIP2-City MMDB mounted into every Traefik pod.
- A known immediate-peer trust boundary for both Traefik entry points and this
  plugin.
- Routes that actually pass through Traefik. The audited cloudflared routes
  targeted backend Services directly and cannot be protected by this
  Middleware until their topology changes.

The audited cluster currently runs Traefik v3.7.1, Helm chart 40.2.0, three
ready replicas, and `externalTrafficPolicy: Cluster`.

## 1. Enable the released plugin statically

Merge this into the Traefik Helm values and replace `vX.Y.Z` with the new
release tag:

```yaml
experimental:
  abortOnPluginFailure: true
  plugins:
    stateGeoBlock:
      moduleName: github.com/vikewoods/traefik-plugin-state-geo
      version: vX.Y.Z
```

Static plugin changes recreate Traefik pods. Keep the alias `stateGeoBlock`
consistent with `spec.plugin.stateGeoBlock` below. The version is required for
published plugins. With the audited zero-unavailable rolling strategy,
`abortOnPluginFailure: true` prevents a broken new plugin pod from replacing
healthy old replicas.

## 2. Mount the database PVC

The audited PVC is `traefik-geolite-db`, is RWX/Longhorn-backed, and is already
mounted read-only at `/data/geolite`:

```yaml
deployment:
  additionalVolumes:
    - name: geolite-db
      persistentVolumeClaim:
        claimName: traefik-geolite-db

additionalVolumeMounts:
  - name: geolite-db
    mountPath: /data/geolite
    readOnly: true
```

The expected database path is
`/data/geolite/GeoLite2-City.mmdb`. At audit time it was 65,864,808 bytes, had
SHA-256 `e2765534f9fc6e0bcda4c46d8bc58bfac5feea6ca2d5581219e53c99cd3b073d`,
was built on 2026-07-14, and declared IP version 6 (IPv4 and IPv6 records).
Those values are evidence, not permanent assertions.

The updater should write a temporary complete database and atomically rename
it over the live file. The plugin checks size and modification time once per
`databaseReloadInterval`, validates a new reader before swapping it, retains
the last known-good reader on failure, and clears decisions after success.

## 3. Preserve forwarding chains in Traefik

Traefik's entry-point trust and the plugin's trust are separate controls. The
audited values already include the node network in
`forwardedHeaders.trustedIPs`:

```yaml
ports:
  web:
    forwardedHeaders:
      insecure: false
      trustedIPs:
        - 10.17.1.0/24
        # Keep the configured Cloudflare IPv4 and IPv6 ranges as applicable.
  websecure:
    forwardedHeaders:
      insecure: false
      trustedIPs:
        - 10.17.1.0/24
        # Keep the configured Cloudflare IPv4 and IPv6 ranges as applicable.
```

Do not use `insecure: true` in production. Because
`externalTrafficPolicy: Cluster` SNATs the socket peer, the node CIDR is needed
to preserve the useful `X-Forwarded-For` chain. The audited `web` entry point
omitted Cloudflare IPv6 ranges while `websecure` included them; retain or align
those ranges based on which entry point handles real application requests.

Traefik rewrites `X-Real-IP` to the immediate peer and normally injects
`X-Forwarded-For` before this Middleware runs. Prefer provider headers or XFF.
If RFC `Forwarded` is authoritative for a route, put it before XFF in that
Middleware's `clientIPHeaders`.

## 4. Create a strict shared Middleware

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: state-geo-block
  namespace: traefik-system
spec:
  plugin:
    stateGeoBlock:
      dbPath: /data/geolite/GeoLite2-City.mmdb
      databaseReloadInterval: 1m
      cacheSize: 1000
      cacheTTL: 15m
      trustedProxyCIDRs:
        - 10.17.1.0/24
      clientIPHeaders:
        - CF-Connecting-IP
        - True-Client-IP
        - X-Forwarded-For
        - Forwarded
        - X-Real-IP
      blockNonUS: true
      blockUSStates: true
      blockedStates:
        - CA
        - NY
      databaseFailurePolicy: deny
      lookupFailurePolicy: deny
      invalidClientIPPolicy: deny
      unknownCountryPolicy: deny
      unknownSubdivisionPolicy: deny
      privateIPPolicy: deny
      whitelistedPaths:
        - /health
      whitelistedPathPrefixes:
        - /.well-known
      logLevel: warn
      logClientIP: false
```

This strict posture denies traffic when expected client identity or geography
is absent. Choose `allow` policies deliberately for services that prioritize
availability over enforcement. Path and IP bypasses are evaluated before
database failure policy, so a tightly scoped health endpoint remains usable.

The audited CRD provider has `allowCrossNamespace: true`, so application
IngressRoutes can reference this shared Middleware. A cluster without that
setting should create the Middleware in each application's namespace.

## 5. Attach it to a route

IngressRoute reference:

```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: app
  namespace: default
spec:
  entryPoints:
    - websecure
  routes:
    - kind: Rule
      match: Host(`app.example.com`)
      middlewares:
        - name: state-geo-block
          namespace: traefik-system
      services:
        - name: app
          port: 80
```

Standard Ingress annotation:

```yaml
metadata:
  annotations:
    traefik.ingress.kubernetes.io/router.middlewares: traefik-system-state-geo-block@kubernetescrd
```

Attaching the Middleware to one router does not affect other routers. Verify
every intended public route and every alternate path such as tunnels.

## 6. Read-only preflight checks

Use the audited context explicitly:

```bash
kubectl --context admin@vs-k8s-01 -n traefik-system get deploy,po,svc,pvc
kubectl --context admin@vs-k8s-01 -n traefik-system get middleware.traefik.io
kubectl --context admin@vs-k8s-01 -n traefik-system get pods \
  -l app.kubernetes.io/name=traefik
```

Before a canary, revalidate:

- Traefik image, chart, pod readiness, and plugin download/startup logs;
- the node CIDR actually seen in Traefik `RemoteAddr`/access logs;
- both entry points' complete Cloudflare IPv4 and IPv6 trusted ranges;
- PVC binding, mount path, file size, readable permissions, MMDB metadata,
  checksum consistency across replicas, and updater success;
- route topology, especially cloudflared or other paths that bypass Traefik.

## 7. Canary matrix

Attach the Middleware to a controlled host first and verify:

| Case | Expected strict result |
| --- | --- |
| Cloudflare IPv4 in allowed US state | backend response |
| Cloudflare IPv6 in allowed US state | backend response |
| Cloudflare client in blocked state | 403 |
| Known non-US client | 403 |
| Valid trusted XFF fallback | geography-dependent result |
| Malformed preferred header plus valid XFF | XFF fallback result |
| Direct/untrusted peer with forged provider header | forged header ignored |
| Missing client header with private SNAT peer | 403 |
| Whitelisted exact health path | backend response |
| Similar path such as `/healthz` | normal geography policy |
| Database temporarily unreadable after a good load | last known-good reader |
| Initial database unavailable | selected database failure policy |

Observe status codes, warnings, latency, memory per pod, and false positives.
The audited traffic was approximately 63.2% IPv6 client identities, so IPv6 is
a mandatory canary case.

## Rollback

1. Detach the Middleware from the canary router.
2. If needed, restore the previous versioned plugin tag in Helm values.
3. Roll Traefik back through the normal Helm/GitOps process.
4. Keep the PVC and updater intact; they are independently healthy.

Do not delete the previous release tag. Versioned plugin rollback depends on a
stable immutable tag.
