# Example: pbs-sync-auth behind HAProxy

A `docker-compose` setup that puts [HAProxy](https://www.haproxy.org) in front of
the `pbs-sync-auth` server and terminates TLS. The Go server stays plain HTTP on
`:8099` internally; HAProxy serves HTTPS on `:443` and redirects HTTP to HTTPS.

You bring your own certificate (see step 2).

    haproxy.cfg          frontend/backend + TLS termination
    docker-compose.yml   HAProxy + the auth server

## Steps

1. Create the shared secret (identical to the one on the client / source PBS):

       openssl rand -hex 32 > secret.key
       chmod 600 secret.key

2. Provide the certificate. HAProxy expects the **full chain and the private key
   in a single PEM** at `certs/haproxy.pem`:

       mkdir -p certs
       # from an existing Let's Encrypt cert:
       cat fullchain.pem privkey.pem > certs/haproxy.pem
       # or a self-signed one for testing:
       openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
         -keyout /tmp/k.pem -out /tmp/c.pem -subj "/CN=pbs-sync-auth.example.com"
       cat /tmp/c.pem /tmp/k.pem > certs/haproxy.pem && rm /tmp/c.pem /tmp/k.pem

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
