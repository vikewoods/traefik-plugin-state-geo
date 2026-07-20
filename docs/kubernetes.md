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
- A compatible MMDB mounted into every Traefik pod: MaxMind GeoLite2-City,
  GeoIP2-City, or a compact stategeodb compliance artifact.
- A known immediate-peer trust boundary for both Traefik entry points and this
  plugin.
- Routes that actually pass through Traefik. The audited cloudflared routes
  targeted backend Services directly and cannot be protected by this
  Middleware until their topology changes.

The audited cluster currently runs Traefik v3.7.1, Helm chart 40.2.0, three
ready replicas, and `externalTrafficPolicy: Cluster`.

## 1. Enable the released plugin statically

Merge this into the Traefik Helm values. The example pins the immutable release
candidate and must be changed deliberately for a later release:

```yaml
experimental:
  abortOnPluginFailure: true
  plugins:
    stateGeoBlock:
      moduleName: github.com/vikewoods/traefik-plugin-state-geo/v2
      version: v2.0.0-rc.2
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

The audited database path is `/data/geolite/GeoLite2-City.mmdb`. At audit time
it was 65,864,808 bytes, had
SHA-256 `e2765534f9fc6e0bcda4c46d8bc58bfac5feea6ca2d5581219e53c99cd3b073d`,
was built on 2026-07-14, and declared IP version 6 (IPv4 and IPv6 records).
Those values are GeoLite2 deployment evidence, not permanent assertions and
not evidence that a compact artifact was deployed to the audited cluster.

A compact stategeodb artifact can instead be mounted at, for example,
`/data/geolite/stategeodb.mmdb`. Set the Middleware `dbPath` to the exact path
chosen for the mounted file. Native and interpreted compatibility tests cover
the compact format, but its Longhorn synchronization, reload timing, file
permissions, proxy path, and pod-memory behavior still require the same cluster
canary as any replacement artifact. A mode such as `0644` is suitable when the
updater and Traefik use different runtime UIDs, provided every parent directory
is traversable by the Traefik process.

The updater must write a temporary complete database and atomically rename it
over the live file. The plugin checks size and modification time once per
`databaseReloadInterval`, validates a new reader before swapping it, retains
the last known-good reader on failure, and clears older decisions after a
successful generation advance.

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
`X-Forwarded-For` before this Middleware runs. The shared production preset
uses only XFF. Do not add a provider-specific header to a route unless its
upstream guarantees that direct clients cannot preserve or inject that header.
If RFC `Forwarded` is authoritative for a route, put it before XFF in that
Middleware's `clientIPHeaders`.

With `externalTrafficPolicy: Cluster`, trusting the node CIDR is necessary for
the observed topology but cannot prove whether a request reached the node from
Cloudflare or directly. This limitation applies to XFF as well as provider
headers: a direct client whose header survives to a node that Traefik trusts
can influence the chain. Restrict origin reachability to the intended proxy,
sanitize at a trusted upstream, or use separate entry points/routes where
source authenticity matters.

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
      cacheSize: 50000
      cacheTTL: 15m
      trustedProxyCIDRs:
        - 10.17.1.0/24
      clientIPHeaders:
        - X-Forwarded-For
      rejectInvalidClientIPHeaders: true
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
      logLevel: info
      logClientIP: false
```

This strict posture denies traffic when expected client identity or geography
is absent. Choose `allow` policies deliberately for services that prioritize
availability over enforcement. Path and IP bypasses are evaluated before
database failure policy, so a tightly scoped health endpoint remains usable.
`logLevel: info` makes 403 policy decisions visible during cutover without
logging client addresses unless `logClientIP` is explicitly enabled.

For a Cloudflare-only route whose origin cannot be reached directly and where
Cloudflare overwrites the provider header, use a separate Middleware with:

```yaml
clientIPHeaders:
  - CF-Connecting-IP
  - X-Forwarded-For
rejectInvalidClientIPHeaders: true
```

`True-Client-IP` is an opt-in Cloudflare Enterprise transform and should only
be configured when the zone actually enables it.

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
| Preferred provider header absent plus valid XFF | XFF result |
| Present malformed trusted header plus valid XFF | 403 in strict mode |
| Direct/untrusted peer with forged provider header | forged header ignored |
| Missing client header with private SNAT peer | 403 |
| Whitelisted exact health path | backend response |
| Similar path such as `/healthz` | normal geography policy |
| Database temporarily unreadable after a good load | last known-good reader |
| Initial database unavailable | selected database failure policy |

Observe status codes, warnings, latency, memory per pod, and false positives.
The audited traffic was approximately 63.2% IPv6 client identities, so IPv6 is
a mandatory canary case.

The vendored pure-Go reader retains the complete selected MMDB in a Go byte
slice and can temporarily overlap old and new slices during reload. The tested
compact compliance artifact was 16,419,258 bytes (about 16.4 MB), while the
audited GeoLite2 file above was 65,864,808 bytes. These are artifact sizes, not
measured Traefik pod RSS. Size the canary from the selected artifact plus the
measured Traefik baseline, then observe initial load and a forced atomic
replacement. The `cacheSize: 50000` value is a high-cardinality ingress
starting point; tune it from observed heap use and cache effectiveness because
every Middleware owns an independent cache.

On 2026-07-19, the three live Traefik containers used 91, 93, and 98 MiB before
the plugin was enabled. The deployment requested 192 MiB and limited memory to
512 MiB. Treat those observations only as the audited baseline; choose and
validate canary resources from the artifact actually mounted rather than
carrying forward an unmeasured universal RSS recommendation.

As the final go-live gate, send a request from a known blocked-state source
through the real external load-balancer path and require a 403. That validates
the service traffic policy, node trust, entry-point header processing, plugin
resolver, MMDB, and Middleware attachment together.

## Rollback

1. Detach the Middleware from the canary router.
2. If needed, restore the previous versioned plugin tag in Helm values.
3. Roll Traefik back through the normal Helm/GitOps process.
4. Keep the PVC and updater intact; they are independently healthy.

Do not delete the previous release tag. Versioned plugin rollback depends on a
stable immutable tag.
