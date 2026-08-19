# PBS setup: source PBS (push source)

Setting up the **source** Proxmox Backup Server that mirrors its backups to the
offsite target via push sync — guarded by the challenge-response auth client.

Roles:
- **Source:** this PBS, local datastore `source-datastore`
- **Target:** the target PBS, datastore `DATASTORE`, namespace `NAMESPACE`
  (creating the namespace, user/token, permissions and reading the fingerprint
  happens there: see [`pbs-target.md`](pbs-target.md))

> Placeholders to replace with your own values: `source-datastore`, `DATASTORE`,
> `NAMESPACE`, the target hostname, `TOKEN_SECRET`, the fingerprint, and the
> `pbs-sync-auth.example.com` auth-server URL.

## 0. Prerequisite
Push sync requires PBS **>= 3.2** on the source (and >= 2.2 on the target for
namespace support). Check the version if needed:

    proxmox-backup-manager version

## 1. Create a remote pointing at the target
Requires the token ID + secret and the fingerprint from the target PBS (see the
target docs, steps 4 and 6):

    proxmox-backup-manager remote create target \
      --host target-pbs.example.com \
      --auth-id 'sync@pbs!push' \
      --password 'TOKEN_SECRET' \
      --fingerprint 'aa:bb:cc:...'

If your version does not know `--auth-id` on `remote create`, use `--userid`
(same value).

## 2. Create the push sync job (without a schedule)
Local datastore `source-datastore` (root namespace) -> target `DATASTORE`,
namespace `NAMESPACE`. **No** schedule, so that only the auth client triggers the
job:

    proxmox-backup-manager sync-job create offsite-push \
      --store source-datastore \
      --remote target \
      --remote-store DATASTORE \
      --remote-ns NAMESPACE \
      --max-depth 0 \
      --sync-direction push \
      --remove-vanished false

`--remote-ns NAMESPACE` pushes the local backups from the root namespace into the
`NAMESPACE` namespace of the target datastore; `--max-depth 0` limits this to the
one level (no sub-namespaces).

Optional bandwidth limit (push uses `rate-out`):

    proxmox-backup-manager sync-job update offsite-push --rate-out 50MiB

The job ID `offsite-push` must match `PBS_SYNC_JOB` in the auth client.

## 3. Install the auth client
Recommended: add the signed APT repo and install (a PBS runs as root, so no
`sudo`) — full details in [`../client/README.md`](../client/README.md):

    curl -fsSL https://ftaeger.github.io/pbs-sync-auth/pubkey.asc \
      | gpg --dearmor > /usr/share/keyrings/pbs-sync-auth.gpg
    echo "deb [signed-by=/usr/share/keyrings/pbs-sync-auth.gpg] \
      https://ftaeger.github.io/pbs-sync-auth stable main" \
      > /etc/apt/sources.list.d/pbs-sync-auth.list
    apt update && apt install pbs-sync-auth-client

    $EDITOR /etc/pbs-sync-auth/client.conf     # PBS_AUTH_URL, PBS_SYNC_JOB, intervals
    install -m 600 secret.key /etc/pbs-sync-auth/secret.key   # same key as the server
    systemctl enable --now pbs-sync-auth.service

## 4. Flow
The client runs as a daemon. Every `PBS_CHECK_INTERVAL` it:
1. runs the mutual challenge-response against the auth server,
2. only continues if the server is genuine AND the client is authorized (both
   proofs valid),
3. if the server has a target-PBS gate enabled and the target is down, logs the
   reason and skips,
4. otherwise, if a backup is due (`PBS_MIN_BACKUP_INTERVAL` since the last
   successful sync) and none of its own runs is in progress, runs
   `proxmox-backup-manager sync-job run offsite-push`.

If the source PBS cannot reach the auth server, or auth fails, the push is skipped
cleanly and the reason is logged to the journal.

## Note: permissions of the local backups
If the regular backup job (writing to `source-datastore`) fails on prune with
`missing Datastore.Modify|Datastore.Prune`: the API token used for it needs
`DatastorePowerUser`. Because of privilege separation, the role must be granted to
**user AND token** (effective privileges = intersection):

    proxmox-backup-manager acl update /datastore/source-datastore DatastorePowerUser --auth-id 'backup@pbs'
    proxmox-backup-manager acl update /datastore/source-datastore DatastorePowerUser --auth-id 'backup@pbs!<token>'

## Note: do not back up the PBS VM into itself
If the PBS runs as a VM on the same Proxmox VE, **exclude** that VM from the local
backup job (`--exclude <vmid>`). Backing up the PBS into its own datastore runs
into an `http upgrade request timed out` and would be worthless in a real
recovery anyway.
