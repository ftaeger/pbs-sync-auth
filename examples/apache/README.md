# Example: pbs-sync-auth behind Apache httpd

A `docker-compose` setup that puts [Apache httpd](https://httpd.apache.org) in
front of the `pbs-sync-auth` server and terminates TLS with `mod_ssl` +
`mod_proxy`. The Go server stays plain HTTP on `:8099` internally; Apache serves
HTTPS on `:443` and redirects HTTP to HTTPS.

You bring your own certificate (see step 2).

    httpd.conf           self-contained config (TLS + reverse proxy)
    docker-compose.yml   Apache + the auth server
    .env.example         DOMAIN

## Steps

1. Create the shared secret (identical to the one on the client / source PBS):

       openssl rand -hex 32 > secret.key
       chmod 600 secret.key

2. Provide the TLS certificate as `certs/fullchain.pem` and `certs/privkey.pem`
   (existing Let's Encrypt cert, internal CA, or self-signed for testing):

       mkdir -p certs
       openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
         -keyout certs/privkey.pem -out certs/fullchain.pem \
         -subj "/CN=$DOMAIN"

3. Configure the environment and start:

       cp .env.example .env      # edit DOMAIN
       docker compose up -d

4. Verify:

       curl https://$DOMAIN/healthz          # -> ok

## Point the client at it

    PBS_AUTH_URL=https://pbs-sync-auth.example.com

Publicly trusted cert → no extra client config. Internal CA → `PBS_AUTH_TLS_CA`;
self-signed test cert → `PBS_AUTH_TLS_VERIFY=false` (testing only). See the
[client README](../../client/README.md).

## Rate limiting & client IP

Apache httpd has no built-in per-IP request rate limit (`mod_ratelimit` only
throttles bandwidth). Options: the third-party `mod_qos` or `mod_evasive`
modules, fail2ban on the access log, or — simplest, if your clients have fixed
IPs — the commented `Require ip` allowlist in `httpd.conf`. Alternatively use one
of the other examples (nginx, HAProxy, Traefik) which rate-limit natively.

`mod_proxy_http` passes the client IP on in `X-Forwarded-For` automatically. The auth server logs every auth result with the client IP (`auth ok for 203.0.113.9`,
`auth denied for …`). It takes that IP from `X-Forwarded-For` only because the
compose file sets `PBS_TRUSTED_PROXIES` to Docker's network range; see the
[server README](../../server/README.md#client-ip-behind-a-reverse-proxy).

`secret.key` and `certs/` are covered by the repository `.gitignore` — never
commit them.
