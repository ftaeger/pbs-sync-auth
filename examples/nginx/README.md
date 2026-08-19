# Example: pbs-sync-auth behind nginx

A `docker-compose` setup that puts [nginx](https://nginx.org) in front of the
`pbs-sync-auth` server and terminates TLS. The Go server stays plain HTTP on
`:8099` internally; nginx serves HTTPS on `:443` and redirects HTTP to HTTPS.

Unlike the [Traefik example](../traefik/), nginx does **not** obtain certificates
itself — you bring your own (see step 2). This keeps the example independent of
any specific ACME setup and works with an existing wildcard cert, an internal CA,
or a self-signed cert for testing.

    templates/default.conf.template   nginx vhost (DOMAIN filled in at startup)
    docker-compose.yml                nginx + the auth server
    .env.example                      DOMAIN

## Steps

1. Create the shared secret (identical to the one on the client / source PBS):

       openssl rand -hex 32 > secret.key
       chmod 600 secret.key

2. Provide the TLS certificate as `certs/fullchain.pem` and `certs/privkey.pem`.
   For example, with an existing Let's Encrypt certificate from certbot:

       mkdir -p certs
       cp /etc/letsencrypt/live/$DOMAIN/fullchain.pem certs/
       cp /etc/letsencrypt/live/$DOMAIN/privkey.pem   certs/

   Or a self-signed cert for local testing (the client then needs
   `PBS_AUTH_TLS_VERIFY=false` or `PBS_AUTH_TLS_CA`, see below):

       mkdir -p certs
       openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
         -keyout certs/privkey.pem -out certs/fullchain.pem \
         -subj "/CN=$DOMAIN"

3. Configure the environment:

       cp .env.example .env
       # edit DOMAIN

4. Start the stack:

       docker compose up -d

5. Verify:

       curl https://$DOMAIN/healthz          # -> ok
       curl -s -X POST https://$DOMAIN/auth/challenge \
         -H 'Content-Type: application/json' -d '{"client_nonce":"0123456789abcdef"}'

## Point the client at it

On the source PBS, set the client to the HTTPS URL:

    PBS_AUTH_URL=https://pbs-sync-auth.example.com

If the certificate is publicly trusted (e.g. Let's Encrypt), the client verifies
it against the system root CAs — no extra config. For an internal CA use
`PBS_AUTH_TLS_CA=/etc/pbs-sync-auth/ca.pem`; for a self-signed test cert use
`PBS_AUTH_TLS_VERIFY=false` (testing only). See the
[client README](../../client/README.md).

## Notes

- `secret.key` and `certs/` are covered by the repository `.gitignore` — never
  commit them.
- The same pattern applies to other proxies (HAProxy, Apache `mod_proxy`, Caddy):
  terminate TLS, then `proxy_pass` / forward to the auth server on port `8099`,
  and pass `/healthz` through for health checks.
