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

## Installation via APT repository (recommended for PBS 4.x)
Add the signed APT repo once; `apt upgrade` then keeps the client up to date.

    curl -fsSL https://ftaeger.github.io/pbs-sync-auth/pubkey.asc \
      | gpg --dearmor | sudo tee /usr/share/keyrings/pbs-sync-auth.gpg >/dev/null

    echo "deb [signed-by=/usr/share/keyrings/pbs-sync-auth.gpg] \
      https://ftaeger.github.io/pbs-sync-auth stable main" \
      | sudo tee /etc/apt/sources.list.d/pbs-sync-auth.list

    sudo apt update && sudo apt install pbs-sync-auth-client

Then configure it (same as below): edit `/etc/pbs-sync-auth/client.conf`, place
`/etc/pbs-sync-auth/secret.key`, and `systemctl enable --now pbs-sync-auth.timer`.
The repo is rebuilt and signed by CI on each release; see
[`../packaging/README.md`](../packaging/README.md).

## Installation via a single Debian package
If you prefer not to add a repo, a prebuilt `.deb` (amd64) is attached to each
GitHub release. It installs the binary to `/usr/bin/pbs-auth-client`, the systemd
`.service`/`.timer`, and a config file at `/etc/pbs-sync-auth/client.conf`. The
shared secret is **not** shipped — you provide it yourself. The package does
**not** start the timer; you enable it once configured.

    sudo apt install ./pbs-sync-auth-client_X.Y.Z_amd64.deb

    # 1) adjust the config (at least PBS_AUTH_URL and PBS_SYNC_JOB)
    sudo $EDITOR /etc/pbs-sync-auth/client.conf

    # 2) place the shared secret (identical to the server's key)
    openssl rand -hex 32 | sudo tee /etc/pbs-sync-auth/secret.key >/dev/null
    sudo chmod 600 /etc/pbs-sync-auth/secret.key

    # 3) enable and start the timer
    sudo systemctl enable --now pbs-sync-auth.timer

Config edits in `/etc/pbs-sync-auth/client.conf` survive package upgrades (it is a
dpkg conffile). The `.deb` is built by CI from `packaging/` — see
[`../packaging/README.md`](../packaging/README.md) to build one locally.

## Manual installation on the PBS
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
    PBS_AUTH_URL        https://pbs-sync-auth.example.com   (http:// also supported)
    PBS_AUTH_SECRET     /etc/pbs-sync-auth/secret.key
    PBS_AUTH_TIMEOUT    3s
    PBS_SYNC_JOB        offsite-push
    PBS_AUTH_TLS_VERIFY true    verify the server certificate (https only); false = test/dev only
    PBS_AUTH_TLS_CA     (unset) optional PEM CA bundle to trust instead of the system roots

### TLS
When `PBS_AUTH_URL` is an `https://` URL, the TLS handshake is the **first gate**:
it happens on the first request, so if certificate verification fails the client
aborts *before* the HMAC round runs. A publicly trusted certificate (e.g. Let's
Encrypt via the [Traefik example](../examples/traefik/)) needs no extra config.
`PBS_AUTH_TLS_CA` lets you trust an internal CA; `PBS_AUTH_TLS_VERIFY=false`
disables verification and is meant for testing only. The mutual HMAC
authentication always runs *in addition* to the TLS check (defense in depth).

## Exit codes
    0  auth ok, sync job started
    1  auth server unreachable / auth failed (sync skipped)
    2  auth ok, but `sync-job run` failed

## PBS-side setup
Remote, push sync job and permissions on the source PBS:
see [`../docs/pbs-source.md`](../docs/pbs-source.md).
