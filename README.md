# pbs-sync-auth

A small authentication gatekeeper that lets a **Proxmox Backup Server (PBS)** run
its offsite **push sync** *only* when it can reach — and cryptographically verify
— a trusted counterpart over the network. It is **not** a backup transport; it
only decides *whether* the push is allowed to run right now.

## Why

A PBS should push its backups to an offsite target only when it is actually on the
trusted network. A plain reachability check (ping/port) is forgeable. Instead, a
client and server perform a **mutual cryptographic authentication**
(HMAC-SHA256 challenge-response). Only when both sides prove they hold the same
shared secret does the client trigger the local push sync job.

Both programs use the **Go standard library only** — no external modules.

## Components

    client/   Go binary on the source PBS, run by a systemd timer (every 30 min).
              On successful auth: `proxmox-backup-manager sync-job run <job>`.
    server/   Go HTTP service, runs as a Docker container on a host reachable
              by the source PBS.
    docs/     PBS-side setup guides (source and target).

## Data flow

    [systemd timer] -> client (source PBS)
        --HTTP/JSON--> server (auth host)          [mutual authentication]
        on OK: client runs `proxmox-backup-manager sync-job run <job>` locally
        --> PBS push sync source -> target (separate, fingerprint-pinned TLS, port 8007)

The auth service transfers **no** backup data. The actual transfer runs
separately over the PBS-native TLS connection, pinned by fingerprint.

## Protocol

Shared secret `K` (32 bytes, hex) in `/etc/pbs-sync-auth/secret.key`, identical on
client and server.

    mac(tag, cn, sn) = HMAC-SHA256(K, "tag|cn|sn")   (tag ∈ {"server","client"})

1. `POST /auth/challenge  {"client_nonce": cn}`
   Server → `{"server_nonce": sn, "server_proof": mac("server", cn, sn)}`
   The client verifies `server_proof` → **the server is genuine**.
2. `POST /auth/verify  {"client_nonce": cn, "server_nonce": sn, "client_proof": mac("client", cn, sn)}`
   Server verifies `client_proof` → `{"status":"ok"}` / `401` → **the client is authorized**.

Security properties: fresh nonces on both sides (replay protection), domain
separation `server`/`client` (no reflection attack), single-use challenges with a
30 s TTL, constant-time comparison (`hmac.Equal`). The authentication is therefore
unforgeable even over plain HTTP — only the random nonces are visible on the wire.

## Quick start (local handshake test)

    # generate the shared secret
    openssl rand -hex 32 > secret.key

    # start the server
    PBS_AUTH_SECRET=./secret.key PBS_AUTH_PORT=8099 go run ./server &

    # run the client against it
    PBS_AUTH_SECRET=./secret.key PBS_AUTH_URL=http://127.0.0.1:8099 go run ./client

See [`server/README.md`](server/README.md) and [`client/README.md`](client/README.md)
for building and deploying, and [`docs/`](docs/) for the PBS-side setup.

## Configuration

Environment variables (see the component READMEs for defaults):

    PBS_AUTH_SECRET   path to the shared secret (default /etc/pbs-sync-auth/secret.key)
    PBS_AUTH_URL      client: auth server base URL
    PBS_AUTH_PORT     server: listen port (default 8099)
    PBS_SYNC_JOB      client: PBS sync job to run (default offsite-push)
    PBS_AUTH_TIMEOUT  client: HTTP timeout (default 3s)

## Roadmap

- Put a TLS-terminating reverse proxy in front of the server (HTTPS/REST), and
  have the client verify the TLS certificate *before* starting the HMAC round
  (defense in depth), with verification toggleable via env. The HMAC auth stays
  **in addition** to the TLS check, not replaced by it.

## Conventions

- **Go: standard library only** — do not add external dependencies.
- Target architecture **linux/amd64**, `CGO_ENABLED=0`, static binary.
- Container: `scratch` base, non-root (65534), read-only; secret mounted
  read-only.
- Never commit the shared secret (`secret.key` is in `.gitignore`).

## License

[MIT](LICENSE) © 2026 Florian Taeger
