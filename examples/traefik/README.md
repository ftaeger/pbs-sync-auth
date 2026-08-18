# Example: pbs-sync-auth behind Traefik

Ready-to-use `docker-compose` setups that put [Traefik](https://traefik.io) in
front of the `pbs-sync-auth` server and terminate TLS with a Let's Encrypt
certificate. The Go server stays plain HTTP on `:8099` internally; Traefik
handles HTTPS on `:443` and redirects HTTP to HTTPS.

Two variants — pick the one that matches your network:

| File | Certificate challenge | Use when |
|------|----------------------|----------|
| `docker-compose.http01.yml` | Let's Encrypt **HTTP-01** | the host is publicly reachable on ports 80/443 |
| `docker-compose.dns01-cloudflare.yml` | Let's Encrypt **DNS-01** (Cloudflare) | the host is **not** publicly reachable; the domain is on Cloudflare |

## Steps

1. Create the shared secret (identical to the one on the client / source PBS):

       openssl rand -hex 32 > secret.key
       chmod 600 secret.key

2. Configure the environment:

       cp .env.example .env
       # edit DOMAIN and ACME_EMAIL
       # for the DNS-01 variant also set CF_DNS_API_TOKEN

3. Start the stack (choose one):

       docker compose -f docker-compose.http01.yml up -d
       # or
       docker compose -f docker-compose.dns01-cloudflare.yml up -d

4. Verify — once the certificate is issued (first request may take a moment):

       curl https://$DOMAIN/healthz          # -> ok
       curl -s -X POST https://$DOMAIN/auth/challenge \
         -H 'Content-Type: application/json' -d '{"client_nonce":"0123456789abcdef"}'

## Point the client at it

On the source PBS, set the client to the HTTPS URL:

    PBS_AUTH_URL=https://pbs-sync-auth.example.com

Because the certificate is a publicly trusted Let's Encrypt certificate, the
client verifies it against the system root CAs — no extra configuration needed.
See the [client README](../../client/README.md) for the `PBS_AUTH_TLS_*` options
(disabling verification for testing, or trusting an internal CA).

## Notes

- The ACME account and issued certificates are stored in `./letsencrypt/acme.json`
  (created on first run). Keep this directory; deleting it re-issues certificates
  and can hit Let's Encrypt rate limits.
- `secret.key` and `letsencrypt/` are covered by the repository `.gitignore` —
  never commit them.
- The Traefik dashboard is intentionally not exposed. Add it yourself only on a
  trusted network if you need it.
