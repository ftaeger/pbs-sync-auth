# PBS setup: target PBS (offsite target)

Setting up the **target** Proxmox Backup Server that receives the push-synced
backups from the source PBS, plus the auth server that guards the push. The auth
server runs on its **own host** (`pbs-sync-auth.example.com`), separate from the
PBS.

Roles:
- **Push target:** the target PBS `target-pbs.example.com`
- **Auth server:** its own host `pbs-sync-auth.example.com` (Docker container)
- **Source:** the source PBS (remote + sync job + auth client there, see
  [`pbs-source.md`](pbs-source.md))

> Backup destination: datastore **`DATASTORE`**, namespace **`NAMESPACE`**.
> Replace these and the hostnames with your own values.

## 0. Prerequisite
Both PBS instances must satisfy the push-sync requirements (source >= 3.2, target
>= 2.2 for namespace support). Check the version if needed:

    proxmox-backup-manager version

## 1. Generate the shared secret (once)
Used by both the auth server (here) and the auth client (on the source PBS):

    openssl rand -hex 32 > secret.key      # chmod 600, never commit to the repo

Transfer a copy securely to the source PBS (see the source docs,
`/etc/pbs-sync-auth/secret.key`).

## 2. Deploy the auth server
Runs on its own host `pbs-sync-auth.example.com`. Recommended: the prebuilt
multi-arch image behind Traefik for TLS — see
[`../examples/traefik/`](../examples/traefik/) and
[`../server/README.md`](../server/README.md) for the container, standalone-binary
and reverse-proxy options.

Quick manual test on that host (plain HTTP, no TLS):

    cd ../server
    cp /path/to/secret.key ./secret.key
    docker compose up -d --build          # listens on :8099

Open the firewall so the source PBS can reach the auth server: **443/tcp** when
running behind Traefik (recommended), or **8099/tcp** for the plain-HTTP setup.

## 3. Create the target namespace
Assuming the datastore `DATASTORE` already exists, create a dedicated namespace
`NAMESPACE` for the pushed backups (keeps them cleanly separated from other
contents of the datastore):

    proxmox-backup-manager namespace create DATASTORE NAMESPACE

Alternatively in the GUI: datastore `DATASTORE` -> add namespace -> `NAMESPACE`.

## 4. Create a dedicated push user + API token
A **dedicated** remote user per push job is recommended (the pushed backups belong
to that user):

    proxmox-backup-manager user create sync@pbs --comment 'Offsite push from source PBS'
    proxmox-backup-manager user generate-token sync@pbs push
    # -> the token secret is shown ONCE: note it down, it goes into the remote
    #    entry on the source PBS (see the source docs).

## 5. Set permissions
The token writes into the target datastore. **Important — privilege separation:**
an API token can never have more privileges than its user (effective privileges =
intersection). Therefore grant the role to **user AND token**, scoped to the
namespace path (more restrictive than the whole datastore):

    proxmox-backup-manager acl update /datastore/DATASTORE/NAMESPACE DatastoreBackup --auth-id 'sync@pbs'
    proxmox-backup-manager acl update /datastore/DATASTORE/NAMESPACE DatastoreBackup --auth-id 'sync@pbs!push'

`DatastoreBackup` is enough because the sync job runs with `--remove-vanished
false` (the target manages its own retention, see step 7). If the push should also
remove vanished snapshots/groups on the target, the remote user needs
`Remote.DatastorePrune` on the **source** (not here).

Verify:

    proxmox-backup-manager acl list

## 6. Print the fingerprint
Needed for the remote entry on the source PBS:

    proxmox-backup-manager cert info | grep Fingerprint

## 7. Retention on the target
Since the push runs deliberately without `remove-vanished`, the target manages its
own retention. On the target PBS, for `DATASTORE` (namespace `NAMESPACE`) set up:
- a **prune job** with the desired policy (e.g. keep-daily/keep-weekly/keep-monthly)
- **garbage collection** (frees space)
- optionally a **verify job** (integrity check)

This way an accidental or malicious deletion on the source PBS cannot take the
offsite copy with it (3-2-1 / ransomware mindset).

## Guarding the push
The push job on the source PBS has **no** schedule of its own; it is triggered
exclusively by the auth client, after both sides have successfully authenticated
against this auth server. The PBS remote additionally pins this target by TLS
fingerprint. Details: [`../README.md`](../README.md).
