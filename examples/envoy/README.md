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

## Rate limiting & client IP

The `local_ratelimit` filter is a single token bucket for the whole listener
(5 requests/s, burst 20, excess → `429`), **not** per client IP — enough to cap a
flood, but one noisy source can use up the budget. Per-IP limits in Envoy need
the global rate-limit service; if your clients have fixed IPs, an
[RBAC filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rbac_filter)
allowlist is the simpler, stronger option.

`use_remote_address: true` makes Envoy append the real client IP to
`X-Forwarded-For`. The auth server logs every auth result with the client IP (`auth ok for 203.0.113.9`,
`auth denied for …`). It takes that IP from `X-Forwarded-For` only because the
compose file sets `PBS_TRUSTED_PROXIES` to Docker's network range; see the
[server README](../../server/README.md#client-ip-behind-a-reverse-proxy).

`secret.key` and `certs/` are covered by the repository `.gitignore` — never
commit them.
