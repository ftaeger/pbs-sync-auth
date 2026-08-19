# packaging — Debian package for the client

Builds the `pbs-sync-auth-client` `.deb` that installs the client on a source
Proxmox Backup Server (PBS 4.x, Debian 13 / amd64). Only the **client** is
packaged; the server is deployed as a container (see [`../server/`](../server/)).

## Installed layout
    /usr/bin/pbs-auth-client                    client binary
    /lib/systemd/system/pbs-sync-auth.service   daemon: auth + gated sync-job
    /etc/pbs-sync-auth/client.conf              config (dpkg conffile, EnvironmentFile)

The shared secret (`/etc/pbs-sync-auth/secret.key`) is **not** shipped: it is
operator-provided, read by the client from disk (never placed in the process
environment), and left untouched even on `purge`. The package **does not** enable
or start the service — the operator does that once the secret and config are in
place (see [`../client/README.md`](../client/README.md)). Upgrading from an older
timer-based package retires the timer automatically.

## Layout of this directory
    debian/control                template (@VERSION@ / @ARCH@ substituted at build)
    debian/conffiles              marks /etc/pbs-sync-auth/client.conf as a conffile
    debian/client.conf            shipped config defaults
    debian/pbs-sync-auth.service  daemon unit (EnvironmentFile), ExecStart=/usr/bin/...
    debian/postinst|prerm|postrm  maintainer scripts (daemon-reload, migration, hint)
    build-deb.sh                  assembles the tree and runs dpkg-deb

## Build
CI (`.github/workflows/build.yml`, job `deb`) builds it on every push and attaches
it to the GitHub release on a `vX.Y.Z` tag. To build locally you need a client
binary and `dpkg-deb`:

    packaging/build-deb.sh <client-binary> <version> [arch] [outdir]
    # e.g.
    packaging/build-deb.sh dist/pbs-auth-client-linux-amd64 1.2.3 amd64 deb-out

On a system without `dpkg-deb` (e.g. macOS), run it inside a Debian container:

    docker run --rm -v "$PWD":/w -w /w debian:trixie \
      bash -c 'apt-get update && apt-get install -y dpkg-dev &&
               packaging/build-deb.sh dist/pbs-auth-client-linux-amd64 1.2.3'

# apt/ — signed APT repository (GitHub Pages)

Publishes the client `.deb` in a signed APT repo at
`https://ftaeger.github.io/pbs-sync-auth/`, so a PBS can `apt install` the client
and get updates via `apt upgrade`. It is rebuilt statelessly from **all** release
`.deb` assets on each `vX.Y.Z` tag (and on `workflow_dispatch`) by the `apt` job
in `build.yml`, then deployed to Pages.

    apt/distributions   reprepro config template (@FPR@ = signing key fingerprint)
    apt/pubkey.asc       public signing key (also served at the Pages root)
    apt/index.html       landing page with the setup snippet
    build-apt-repo.sh    <deb-dir> <out-dir> <fpr> -> signed dists/ + pool/ tree

Signing uses a dedicated ed25519 key: private part in the Actions secret
`GPG_PRIVATE_KEY`, fingerprint in the variable `GPG_KEY_FPR`, public part in
`apt/pubkey.asc`. To build locally you need `reprepro` and the signing key in your
keyring:

    packaging/build-apt-repo.sh <dir-with-debs> public <fingerprint>

Setup instructions for the PBS side are in
[`../client/README.md`](../client/README.md).
