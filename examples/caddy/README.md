# Example: pbs-sync-auth behind Caddy

A `docker-compose` setup that puts [Caddy](https://caddyserver.com) in front of
the `pbs-sync-auth` server and terminates TLS. The Go server stays plain HTTP on
`:8099` internally; Caddy serves HTTPS on `:443` and redirects HTTP to HTTPS.

Caddy can also obtain certificates automatically, but to keep this example in
line with the others it uses a **certificate you provide** (see step 2).

    Caddyfile            reverse proxy + TLS (DOMAIN from the environment)
    docker-compose.yml   Caddy + the auth server
    .env.example         DOMAIN

## Steps

1. Create the shared secret (identical to the one on the client / source PBS):

       openssl rand -hex 32 > secret.key
       chmod 600 secret.key

2. Provide the TLS certificate as `certs/fullchain.pem` and `certs/privkey.pem`
   (an existing Let's Encrypt cert, an internal CA, or a self-signed one for
   testing):

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

Publicly trusted cert → no extra client config. Internal CA →
`PBS_AUTH_TLS_CA`; self-signed test cert → `PBS_AUTH_TLS_VERIFY=false` (testing
only). See the [client README](../../client/README.md).

`secret.key` and `certs/` are covered by the repository `.gitignore` — never
commit them.
