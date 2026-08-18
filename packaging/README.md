# packaging — Debian package for the client

Builds the `pbs-sync-auth-client` `.deb` that installs the client on a source
Proxmox Backup Server (PBS 4.x, Debian 13 / amd64). Only the **client** is
packaged; the server is deployed as a container (see [`../server/`](../server/)).

## Installed layout
    /usr/bin/pbs-auth-client                    client binary
    /lib/systemd/system/pbs-sync-auth.service   oneshot: auth + sync-job run
    /lib/systemd/system/pbs-sync-auth.timer     every 30 min
    /etc/pbs-sync-auth/client.conf              config (dpkg conffile, EnvironmentFile)

The shared secret (`/etc/pbs-sync-auth/secret.key`) is **not** shipped: it is
operator-provided, read by the client from disk (never placed in the process
environment), and left untouched even on `purge`. The package **does not** enable
or start the timer — the operator does that once the secret and config are in
place (see [`../client/README.md`](../client/README.md)).

## Layout of this directory
    debian/control                template (@VERSION@ / @ARCH@ substituted at build)
    debian/conffiles              marks /etc/pbs-sync-auth/client.conf as a conffile
    debian/client.conf            shipped config defaults
    debian/pbs-sync-auth.service  EnvironmentFile-based unit, ExecStart=/usr/bin/...
    debian/pbs-sync-auth.timer
    debian/postinst|prerm|postrm  maintainer scripts (daemon-reload, activation hint)
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
