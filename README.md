# Using the `traefik-plugin-state-geo` in Docker Swarm with Traefik v3

### 1. The Directory Structure on your Manager Nodes
Traefik expects local plugins to follow a very specific directory structure based on the Go module name.

### 2. Update your `docker-compose.yml`
You need to mount the plugin source code and the database file into the Traefik container and enable the experimental local plugin feature.

```yaml
services:
  traefik:
    image: traefik:v3.0
    command:
      # ... your existing commands ...
      - "--experimental.localPlugins.stateblock.moduleName=github.com/vikewoods/traefik-plugin-state-geo"
      - "--providers.docker.swarmMode=true"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      # Mount the entire plugins-local directory
      - /mnt/traefik-data/plugins-local:/plugins-local # <- THIS is the important part
      - /mnt/project-a/custom-block.html:/templates/project-a.html:ro
    deploy:
      placement:
        constraints: [node.role == manager]
```


### 3. How to use it on

**Important:** The `dbPath` inside the label must point to the path **inside the container**. Based on the volume mapping above, the file is at:
`/plugins-local/src/github.com/vikewoods/traefik-plugin-state-geo/data/GeoLite2-City.mmdb`

Add these labels to your application service:

```yaml
services:
  my-website:
    image: my-app:latest
    deploy:
      labels:
        - "traefik.enable=true"
        - "traefik.http.routers.my-app.rule=Host(`example.com`)"
        - "traefik.http.routers.my-app.entrypoints=websecure"
        
        # Define the middleware
        - "traefik.http.middlewares.geo-block.plugin.stateblock.dbPath=/plugins-local/src/github.com/vikewoods/traefik-plugin-state-geo/data/GeoLite2-City.mmdb" # <- THIS is the important part
        - "traefik.http.middlewares.geo-block.plugin.stateblock.templatePath=/plugins-local/src/github.com/vikewoods/traefik-plugin-state-geo/data/blocked.html"
        - "traefik.http.middlewares.project-a-block.plugin.stateblock.templatePath=/templates/project-a.html" # <- This is the path to your custom template
        - "traefik.http.middlewares.geo-block.plugin.stateblock.blockedStates=CA,CT,DE,ID,LA,MI,MS,MT,NJ,NY,NV,WA"
        - "traefik.http.middlewares.geo-block.plugin.stateblock.whitelistedIPs=1.2.3.4,11.22.33.44"
        -  "traefik.http.middlewares.state-blocker.plugin.state-geo.whitelistedPaths[0]=/.well-known/"
        - "traefik.http.middlewares.state-blocker.plugin.state-geo.whitelistedPaths[1]=/health"
        - "traefik.http.middlewares.state-blocker.plugin.state-geo.whitelistedPaths[2]=/api/public/"
        
        # Attach the middleware to the router
        - "traefik.http.routers.my-app.middlewares=geo-block"
```
