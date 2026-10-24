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

Every example also rate-limits the auth endpoints (built-in where the proxy
supports it; Caddy needs a plugin, Apache a third-party module), offers a
commented IP allowlist for clients with fixed addresses, and passes the client IP
in `X-Forwarded-For` so the server can log it (`PBS_TRUSTED_PROXIES`). A source
PBS sends only 2 requests per check interval, so the limits never affect
legitimate clients.

| Example | Rate limit | Per client IP |
|---------|------------|---------------|
| traefik | `ratelimit` middleware, 1 r/s, burst 5 | yes |
| nginx | `limit_req`, 1 r/s, burst 5 | yes |
| haproxy | stick table, > 20 req / 10 s → 429 | yes |
| envoy | `local_ratelimit`, 5 r/s, burst 20 | no (whole listener) |
| caddy | needs the `caddy-ratelimit` plugin | yes |
| apache | not built in (`mod_qos`, fail2ban, or allowlist) | — |

> These compose files are provided as templates. Adapt image versions, hostnames
> and certificate paths to your environment.
