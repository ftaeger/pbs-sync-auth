# Example: pbs-sync-auth behind Envoy

A `docker-compose` setup that puts [Envoy](https://www.envoyproxy.io) in front of
the `pbs-sync-auth` server and terminates TLS. The Go server stays plain HTTP on
`:8099` internally; Envoy serves HTTPS on `:443` and redirects HTTP to HTTPS.

You bring your own certificate (see step 2).

    envoy.yaml           static config (TLS termination + reverse proxy)
    docker-compose.yml   Envoy + the auth server

## Steps

1. Create the shared secret (identical to the one on the client / source PBS):

       openssl rand -hex 32 > secret.key
       chmod 600 secret.key

2. Provide the TLS certificate as `certs/fullchain.pem` and `certs/privkey.pem`
   (existing Let's Encrypt cert, internal CA, or self-signed for testing):

       mkdir -p certs
       openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
         -keyout certs/privkey.pem -out certs/fullchain.pem \
         -subj "/CN=pbs-sync-auth.example.com"

3. Start:

       docker compose up -d

4. Verify:

       curl https://pbs-sync-auth.example.com/healthz          # -> ok

## Point the client at it

    PBS_AUTH_URL=https://pbs-sync-auth.example.com

Publicly trusted cert → no extra client config. Internal CA → `PBS_AUTH_TLS_CA`;
self-signed test cert → `PBS_AUTH_TLS_VERIFY=false` (testing only). See the
[client README](../../client/README.md).

`secret.key` and `certs/` are covered by the repository `.gitignore` — never
commit them.
