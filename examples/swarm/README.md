# Docker Swarm example

Use a released public plugin in Traefik's static configuration:

```yaml
command:
  - --experimental.plugins.stateGeoBlock.modulename=github.com/vikewoods/traefik-plugin-state-geo
  - --experimental.plugins.stateGeoBlock.version=vX.Y.Z
volumes:
  - /mnt/traefik/GeoLite2-City.mmdb:/data/geolite/GeoLite2-City.mmdb:ro
```

Example dynamic labels:

```yaml
deploy:
  labels:
    - traefik.http.routers.app.middlewares=state-geo-block
    - traefik.http.middlewares.state-geo-block.plugin.stateGeoBlock.dbPath=/data/geolite/GeoLite2-City.mmdb
    - traefik.http.middlewares.state-geo-block.plugin.stateGeoBlock.trustedProxyCIDRs=10.17.1.0/24
    - traefik.http.middlewares.state-geo-block.plugin.stateGeoBlock.clientIPHeaders=CF-Connecting-IP,True-Client-IP,X-Forwarded-For,Forwarded,X-Real-IP
    - traefik.http.middlewares.state-geo-block.plugin.stateGeoBlock.blockedStates=CA,NY
    - traefik.http.middlewares.state-geo-block.plugin.stateGeoBlock.databaseFailurePolicy=deny
    - traefik.http.middlewares.state-geo-block.plugin.stateGeoBlock.privateIPPolicy=deny
```

The database and any `templatePath` must be mounted into every node on which a
Traefik task can run. Local plugin source mounts are intended for development;
production should use an immutable released tag.
