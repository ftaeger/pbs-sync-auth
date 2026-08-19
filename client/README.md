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
    systemd/pbs-sync-auth.service  long-running daemon: auth + gated sync-job

## How it runs
The client is a long-running daemon. Every `PBS_CHECK_INTERVAL` it authenticates
against the server; if the server reports the target PBS is unavailable it logs
the reason and skips. Otherwise it starts the push sync job — but only if a backup
is **due** (`PBS_MIN_BACKUP_INTERVAL` since the last successful sync, queried from
PBS) and none of its own runs is in progress (it runs the job synchronously, so it
never overlaps itself). `pbs-auth-client --once` runs a single cycle and exits (see
*Exit codes*), handy for testing.

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

## Installation

> On a PBS you operate as **root**, so the commands below use **no `sudo`**.

### Option A — APT repository (recommended for PBS 4.x)
Add the signed APT repo once; `apt upgrade` then keeps the client up to date.

    curl -fsSL https://ftaeger.github.io/pbs-sync-auth/pubkey.asc \
      | gpg --dearmor > /usr/share/keyrings/pbs-sync-auth.gpg

    echo "deb [signed-by=/usr/share/keyrings/pbs-sync-auth.gpg] \
      https://ftaeger.github.io/pbs-sync-auth stable main" \
      > /etc/apt/sources.list.d/pbs-sync-auth.list

    apt update && apt install pbs-sync-auth-client

The repo is rebuilt and signed by CI on each release; see
[`../packaging/README.md`](../packaging/README.md). Then *Configure and enable*
below.

### Option B — a single Debian package
If you prefer not to add a repo, a prebuilt `.deb` (amd64) is attached to each
GitHub release. It installs the binary to `/usr/bin/pbs-auth-client`, the systemd
`.service`, and a config file at `/etc/pbs-sync-auth/client.conf`. The shared
secret is **not** shipped, and the package does **not** start the service.

    apt install ./pbs-sync-auth-client_X.Y.Z_amd64.deb

Config edits in `/etc/pbs-sync-auth/client.conf` survive package upgrades (it is a
dpkg conffile).

### Configure and enable (Option A or B)

    # 1) adjust the config (PBS_AUTH_URL, PBS_SYNC_JOB, and optionally the
    #    PBS_CHECK_INTERVAL / PBS_MIN_BACKUP_INTERVAL below)
    $EDITOR /etc/pbs-sync-auth/client.conf

    # 2) install the shared secret — the SAME secret.key as on the server
    install -m 600 secret.key /etc/pbs-sync-auth/secret.key

    # 3) enable and start the daemon
    systemctl enable --now pbs-sync-auth.service

Watch it:

    systemctl status pbs-sync-auth.service
    journalctl -u pbs-sync-auth.service -f

### Option C — manual, without a package
Install the binary, secret and the systemd unit by hand:

    install -m 0755 pbs-auth-client /usr/local/bin/pbs-auth-client
    mkdir -p /etc/pbs-sync-auth
    install -m 600 secret.key /etc/pbs-sync-auth/secret.key   # same as the server's
    cp systemd/pbs-sync-auth.service /etc/systemd/system/
    systemctl daemon-reload
    systemctl enable --now pbs-sync-auth.service

The bundled unit carries its configuration inline (no `client.conf`): edit the
`Environment=` lines in `pbs-sync-auth.service` to set `PBS_AUTH_URL`,
`PBS_SYNC_JOB`, etc. (see *Configuration* below).

## Configuration (client.conf / Environment= in pbs-sync-auth.service)
    PBS_AUTH_URL            https://pbs-sync-auth.example.com   (http:// also supported)
    PBS_AUTH_SECRET         /etc/pbs-sync-auth/secret.key
    PBS_AUTH_TIMEOUT        3s
    PBS_SYNC_JOB            offsite-push
    PBS_CHECK_INTERVAL      30m     how often the daemon runs a cycle
    PBS_MIN_BACKUP_INTERVAL 0       min time between successful syncs (0 = every check)
    PBS_AUTH_TLS_VERIFY     true    verify the server certificate (https only); false = test/dev only
    PBS_AUTH_TLS_CA         (unset) optional PEM CA bundle to trust instead of the system roots

Example: check every 2 h but back up at most every 12 h →
`PBS_CHECK_INTERVAL=2h`, `PBS_MIN_BACKUP_INTERVAL=12h`.

### TLS
When `PBS_AUTH_URL` is an `https://` URL, the TLS handshake is the **first gate**:
it happens on the first request, so if certificate verification fails the client
aborts *before* the HMAC round runs. A publicly trusted certificate (e.g. Let's
Encrypt via the [Traefik example](../examples/traefik/)) needs no extra config.
`PBS_AUTH_TLS_CA` lets you trust an internal CA; `PBS_AUTH_TLS_VERIFY=false`
disables verification and is meant for testing only. The mutual HMAC
authentication always runs *in addition* to the TLS check (defense in depth).

## Exit codes (`--once` mode)
    0  synced, or skipped because a backup was not yet due
    1  auth server unreachable / auth failed (sync skipped)
    2  auth ok, but `sync-job run` failed
    3  auth ok, but the target PBS is unavailable (sync skipped)

As a daemon the client does not exit per cycle — it logs the same outcomes to the
journal and continues.

## PBS-side setup
Remote, push sync job and permissions on the source PBS:
see [`../docs/pbs-source.md`](../docs/pbs-source.md).
