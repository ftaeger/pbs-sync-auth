# pbs-sync-auth — server (target side)

Challenge-response auth server for the guarded offsite push. It runs as a Docker
container on a host reachable by the source PBS (default port 8099) and is the
counterpart to the client running on the source PBS.

It proves via HMAC-SHA256 that both sides hold the same shared secret (mutual
authentication). Without knowledge of the secret, neither a forged server can
produce a valid `server_proof` nor a foreign client a valid `client_proof`.

Client counterpart: [`../client/`](../client/).

## Contents
    main.go               Go server (standard library only)
    go.mod
    Dockerfile            multi-stage, runtime on scratch (non-root, read-only)
    docker-compose.yml    local / manual operation

## Endpoints
    POST /auth/challenge  {client_nonce}                          -> {server_nonce, server_proof}
    POST /auth/verify     {client_nonce, server_nonce, client_proof} -> {status:"ok"}
    GET  /healthz                                                 -> 200 "ok"  (health check)

## Deployment

Pick one of the following. In every case the server needs the shared `secret.key`
(see below) and listens on `:8099` (HTTP; put a reverse proxy in front for TLS).

### A) Prebuilt Docker image (recommended)
A multi-arch image is published on every push to `main` and for each release tag.
It runs on **x86_64, arm64 and armv7** — including Raspberry Pi 3/4/5.

    docker run -d --name pbs-auth-server \
      -p 8099:8099 \
      -v "$PWD/secret.key:/run/secrets/pbs_auth_secret:ro" \
      -e PBS_AUTH_SECRET=/run/secrets/pbs_auth_secret \
      --read-only --security-opt no-new-privileges \
      ghcr.io/ftaeger/pbs-sync-auth:latest

Tags: `latest`, `sha-<short>` (per main commit), and `X.Y.Z` / `vX.Y.Z` (releases).

### B) Behind Traefik (TLS)
Ready-to-use `docker-compose` setups with Let's Encrypt (HTTP-01 or Cloudflare
DNS-01) are in [`../examples/traefik/`](../examples/traefik/).

### C) Standalone binary + systemd
Download the `pbs-auth-server-linux-<arch>` binary from the
[releases](https://github.com/ftaeger/pbs-sync-auth/releases) (or build it, below):

    sudo install -m 0755 pbs-auth-server-linux-amd64 /usr/local/bin/pbs-auth-server
    sudo mkdir -p /etc/pbs-sync-auth
    sudo cp secret.key /etc/pbs-sync-auth/secret.key && sudo chmod 600 /etc/pbs-sync-auth/secret.key
    sudo cp systemd/pbs-sync-auth-server.service /etc/systemd/system/
    sudo systemctl daemon-reload && sudo systemctl enable --now pbs-sync-auth-server.service
    systemctl status pbs-sync-auth-server.service

### Build the image locally (manual)
On an x86_64 host directly:

    cp /path/to/secret.key ./secret.key
    docker compose up -d --build

Build a multi-arch image with buildx:

    docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 \
      -t pbs-auth-server:latest .

## Configuration (environment)
    PBS_AUTH_HOST     0.0.0.0
    PBS_AUTH_PORT     8099
    PBS_AUTH_SECRET   /run/secrets/pbs_auth_secret   (file mounted read-only)

## Test
    curl -s -X POST http://localhost:8099/auth/challenge \
      -H 'Content-Type: application/json' -d '{"client_nonce":"0123456789abcdef"}'
    # -> returns server_nonce + server_proof

## Shared secret
Generate once and distribute to BOTH sides (server here, client on the source PBS):

    openssl rand -hex 32 > secret.key   # chmod 600, never commit to the repo

## PBS-side setup (target)
Namespace, user/token creation and permissions on the target PBS:
see [`../docs/pbs-target.md`](../docs/pbs-target.md).
