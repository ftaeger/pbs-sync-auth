# pbs-sync-auth — client (source PBS)

Auth client for the guarded offsite push from a source Proxmox Backup Server to a
target Proxmox Backup Server.

The client performs a mutual challenge-response (HMAC-SHA256) against the auth
server. **Only** if both sides authenticate securely — the server is genuine and
confirms the client as authorized — does the client trigger the push sync job. So
the push only runs when the source PBS can actually reach the trusted network.

Counterpart (server): see [`../server/`](../server/).

## Contents
    main.go                       Go client (standard library only)
    go.mod
    build-with-docker.sh          builds a static linux/amd64 binary via Docker
    systemd/pbs-sync-auth.service  oneshot: auth + sync-job run
    systemd/pbs-sync-auth.timer    checks every 30 min

## Protocol
1. Client -> server  POST /auth/challenge {client_nonce}
2. Server -> client  {server_nonce, server_proof=HMAC(K,"server|cn|sn")}
   Client verifies server_proof -> the server is genuine.
3. Client -> server  POST /auth/verify {cn, sn, client_proof=HMAC(K,"client|cn|sn")}
   Server verifies client_proof -> the client is authorized.

Fresh nonces on both sides (replay protection), domain separation
"server"/"client" (no reflection), single-use challenges (TTL 30 s),
constant-time comparison.

## Build
The target architecture is fixed to **linux/amd64** (PBS is x86_64). The build
also works on an Apple Silicon Mac, since Go cross-compiles:

    ./build-with-docker.sh          # result: ./pbs-auth-client

Without Docker, using a local Go toolchain:

    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o pbs-auth-client .

## Installation on the PBS
    sudo cp pbs-auth-client /usr/local/bin/ && sudo chmod +x /usr/local/bin/pbs-auth-client

Place the shared secret (identical to the server's, see the target docs):

    sudo mkdir -p /etc/pbs-sync-auth
    sudo cp secret.key /etc/pbs-sync-auth/secret.key
    sudo chmod 600 /etc/pbs-sync-auth/secret.key

systemd timer:

    sudo cp systemd/pbs-sync-auth.service systemd/pbs-sync-auth.timer /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now pbs-sync-auth.timer

Manual test:

    sudo systemctl start pbs-sync-auth.service
    journalctl -u pbs-sync-auth.service -n 20

## Configuration (environment, set in pbs-sync-auth.service)
    PBS_AUTH_URL      http://pbs-sync-auth.example.com:8099
    PBS_AUTH_SECRET   /etc/pbs-sync-auth/secret.key
    PBS_AUTH_TIMEOUT  3s
    PBS_SYNC_JOB      offsite-push

## Exit codes
    0  auth ok, sync job started
    1  auth server unreachable / auth failed (sync skipped)
    2  auth ok, but `sync-job run` failed

## PBS-side setup
Remote, push sync job and permissions on the source PBS:
see [`../docs/pbs-source.md`](../docs/pbs-source.md).
