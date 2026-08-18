# CLAUDE.md — project context for AI-assisted work

This repo is the tooling for a **guarded offsite push** of Proxmox Backup Server
backups. It is a pure authentication gatekeeper, **not** a backup transport.

## Purpose / big picture

A source Proxmox Backup Server (PBS) backs up local VMs and should additionally
mirror those backups to a second, offsite PBS via PBS push sync — **but only**
when the source is actually on the trusted network and the counterpart is genuine.

A plain reachability check (ping/port) would be forgeable. Instead, client and
server perform a **mutual cryptographic authentication** (HMAC-SHA256
challenge-response). Only when both sides prove the same shared secret does the
client trigger the push sync job on the source PBS.

## Components

    server/   Go HTTP service, runs as a Docker container on a host reachable by
              the source PBS (default port 8099).
    client/   Go binary on the source PBS, run by a systemd timer (every 30 min).
              On successful auth: `proxmox-backup-manager sync-job run <job>`.
    docs/     PBS-side setup guides (source and target).

Both Go programs use the **standard library only** (no external modules).

## Data flow

    [systemd timer] -> client (source PBS)
        --HTTP/JSON--> server (auth host)          [mutual authentication]
        on OK: client runs `proxmox-backup-manager sync-job run <job>` locally
        --> PBS push sync source -> target (separate, fingerprint-pinned TLS, port 8007)

The auth service transfers **no** backup data. The actual transfer runs separately
over the PBS-native TLS connection, pinned by fingerprint.

## Protocol

Shared secret `K` at `/etc/pbs-sync-auth/secret.key` (32 bytes, hex), identical on
client and server.

    mac(tag, cn, sn) = HMAC-SHA256(K, "tag|cn|sn")   (tag ∈ {"server","client"})

1. `POST /auth/challenge  {"client_nonce": cn}`
   Server → `{"server_nonce": sn, "server_proof": mac("server", cn, sn)}`
   Client verifies server_proof → **server is genuine**.
2. `POST /auth/verify  {"client_nonce": cn, "server_nonce": sn, "client_proof": mac("client", cn, sn)}`
   Server verifies client_proof → `{"status":"ok"}` / `401` → **client is authorized**.

Security properties: fresh nonces on both sides (replay protection), domain
separation `server`/`client` (no reflection attack), single-use challenges with a
30 s TTL, constant-time comparison (`hmac.Equal`). The auth is therefore
unforgeable even over plain HTTP — only the random nonces are visible.

## Current state

- Server listens on internal **plain HTTP :8099** (`PBS_AUTH_PORT`); TLS is
  terminated by a reverse proxy in front (see the Traefik example).
- Endpoints: `POST /auth/challenge`, `POST /auth/verify`, `GET /healthz`.
- Client supports `https://` `PBS_AUTH_URL` with system-root verification; TLS is
  the first gate before the HMAC round. Toggles: `PBS_AUTH_TLS_VERIFY`
  (default true), `PBS_AUTH_TLS_CA` (optional internal-CA bundle).
- Packaging: multi-arch server image (`amd64`, `arm64`, `arm/v7`, incl. Raspberry
  Pi 3/4/5) on `ghcr.io/ftaeger/pbs-sync-auth`; static release binaries for
  server and client. Built by GitHub Actions (`.github/workflows/`): `ci.yml`
  validates PRs; `build.yml` builds/pushes on `main` and cuts a Release on a
  `vX.Y.Z` tag.
- Example deployment with Traefik + Let's Encrypt (HTTP-01 and Cloudflare DNS-01)
  in `examples/traefik/`.

## Roadmap / possible next steps

The HTTPS-via-reverse-proxy goal and the client-side TLS gate are **done** (see
Current state). Remaining ideas, only if a concrete need arises:

- SPKI/public-key pinning for the client (deliberately **not** implemented: leaf
  pinning breaks on Let's Encrypt's ~90-day renewals, and publicly trusted certs
  make it unnecessary).
- Rate limiting / basic abuse protection on the server endpoints.
- Optional structured/JSON logging.

Always **preserve** the existing security properties (see Protocol) and keep the
HMAC auth **in addition** to any TLS check (defense in depth), never instead.

## Build & test

Server (local/standalone):

    cd server
    cp /path/to/secret.key ./secret.key
    docker compose up -d --build            # listens on :8099

Client (static linux/amd64 binary, also buildable on Apple Silicon):

    cd client
    ./build-with-docker.sh                  # -> ./pbs-auth-client

Local handshake test (generate secret, start server, run client):

    openssl rand -hex 32 > secret.key
    PBS_AUTH_SECRET=./secret.key PBS_AUTH_PORT=8099 go run ./server &
    PBS_AUTH_SECRET=./secret.key PBS_AUTH_URL=http://127.0.0.1:8099 go run ./client

## Conventions

- **Go: standard library only**, do not add external dependencies.
- Target architecture **linux/amd64** (`CGO_ENABLED=0`, static binary).
- Container: `scratch` base, non-root (65534), read-only; secret mounted
  read-only.
- All code, comments and docs in **English**.
- **Never** commit the shared secret (`secret.key` is in `.gitignore`).
- This is a standalone, generic open-source project — keep it free of any
  organization-, host- or site-specific references; use placeholders
  (`pbs-sync-auth.example.com`, `DATASTORE`, `NAMESPACE`, …) in docs.

## Deployment

Deployment/orchestration (Ansible, Terraform, etc.) is intentionally out of scope
for this repo — it only provides the source components (Go code + container build)
and the PBS-side setup guides in `docs/`.
