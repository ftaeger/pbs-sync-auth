# Reverse-proxy examples

The `pbs-sync-auth` server speaks plain HTTP on `:8099`. Put a reverse proxy in
front to terminate TLS. Each subdirectory is a self-contained `docker-compose`
setup that runs the same auth server container and mounts the shared `secret.key`
read-only; the client always talks to the proxy over HTTPS on `:443`.

| Example | Proxy | TLS |
|---------|-------|-----|
| [traefik/](traefik/) | Traefik | automatic Let's Encrypt (HTTP-01 or Cloudflare DNS-01) |
| [nginx/](nginx/) | nginx | bring your own certificate |
| [caddy/](caddy/) | Caddy | bring your own certificate |
| [haproxy/](haproxy/) | HAProxy | bring your own certificate |
| [apache/](apache/) | Apache httpd | bring your own certificate |
| [envoy/](envoy/) | Envoy | bring your own certificate |

The "bring your own certificate" examples expect `certs/fullchain.pem` and
`certs/privkey.pem` (HAProxy: a single concatenated `certs/haproxy.pem`). Use an
existing Let's Encrypt certificate, an internal CA, or a self-signed cert for
testing — see each example's README.

> These compose files are provided as templates. Adapt image versions, hostnames
> and certificate paths to your environment.
