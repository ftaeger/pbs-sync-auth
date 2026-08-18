# Documentation

PBS-side setup guides for the guarded offsite push. All hostnames, datastore and
namespace names are placeholders — replace them with your own.

- [`pbs-source.md`](pbs-source.md) — **source PBS** (push source): remote to the
  target, push sync job without a schedule, installing the auth client.
- [`pbs-target.md`](pbs-target.md) — **target PBS** (offsite target): namespace,
  push user/token, permissions, fingerprint, retention; plus deploying the auth
  server.

For the project overview and the auth protocol, see the top-level
[`../README.md`](../README.md).
