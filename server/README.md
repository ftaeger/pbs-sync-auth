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

## Build & run (manual)
On an x86_64 host directly:

    cp /path/to/secret.key ./secret.key
    docker compose up -d --build

Build the image for x86_64 on an Apple Silicon Mac:

    docker buildx build --platform linux/amd64 -t pbs-auth-server:latest --load .

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
