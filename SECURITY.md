# Security policy

## Supported versions

Only the latest release of the current major version (`1.x`) receives fixes.
The server image tag `:1` always points to it.

## Reporting a vulnerability

Please **do not** open a public issue. Report vulnerabilities privately via
[GitHub's private vulnerability reporting](https://github.com/ftaeger/pbs-sync-auth/security/advisories/new)
(Security tab → "Report a vulnerability").

Please include the affected component (server, client, packaging, CI), the
version, and steps to reproduce. You can expect an initial response within a
week; fixes are released as a patch version and announced in the
[changelog](CHANGELOG.md) and a GitHub security advisory.

In scope are in particular weaknesses in the mutual authentication protocol
(see the README), the release/packaging pipeline (image, binaries, `.deb`, APT
repository) and anything that could let a sync run without a successful
handshake.

## Verifying releases

Release binaries, the client `.deb` and the server image carry signed build
provenance attestations. Verify them with the GitHub CLI:

    gh attestation verify pbs-auth-client-linux-amd64 -R ftaeger/pbs-sync-auth
    gh attestation verify oci://ghcr.io/ftaeger/pbs-sync-auth:1 -R ftaeger/pbs-sync-auth

The APT repository is signed with the key in
[`packaging/apt/pubkey.asc`](packaging/apt/pubkey.asc).
