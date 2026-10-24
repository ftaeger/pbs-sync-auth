# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

Each release ships the server image (`ghcr.io/ftaeger/pbs-sync-auth`), static
binaries and the client `.deb` (also via the [APT repository](https://apt.pbs-sync-auth.com/)).
The section for a version is used as the text of its GitHub release.

## [Unreleased]

## [1.0.1] - 2026-10-24

No changes to the server or client code; the protocol is unchanged. This
release is about how pbs-sync-auth is built, published and verified.

### Changed
- Server image: releases are now also tagged with the floating major tag
  (`:1`), alongside `:X.Y.Z` and `:X.Y`, so you can pin a major line and still
  receive patch and security updates. Docs and examples use `:1`.
- The APT repository moved to `https://apt.pbs-sync-auth.com/`. The old
  `https://ftaeger.github.io/pbs-sync-auth/` URL redirects there, so existing
  sources keep working; updating `/etc/apt/sources.list.d/pbs-sync-auth.list`
  is recommended.
- Server image: dropped the redundant `v`-prefixed tag (`:vX.Y.Z`); use
  `:X.Y.Z`. Pre-release tags no longer move `:latest`.

### Security
- Release binaries, the client `.deb` and the server image now carry signed
  build provenance attestations (`gh attestation verify`, see `SECURITY.md`).
- CI: all GitHub Actions and the Go builder image are pinned to immutable
  digests (kept current by Dependabot); workflow token permissions reduced to
  the minimum per job; checkouts no longer persist credentials.
- Added `SECURITY.md` (private vulnerability reporting) and an OpenSSF
  Scorecard workflow.

### Added
- `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, issue forms and a pull request
  template.

## [1.0.0] - 2026-10-24

First stable release. Upgrading is **strongly recommended**: all earlier
releases were built with Go 1.23 (end of life), and `govulncheck` reports 34
reachable standard-library vulnerabilities (`crypto/tls`, `crypto/x509`,
`net/http`, …) in each of their binaries. 1.0.0 is built with Go 1.27.3 and has
none. The protocol is unchanged; 1.0.0 clients and servers interoperate with
0.0.x.

### Security
- Build server, client, image and `.deb` with Go 1.27 (`check-latest`); `go.mod`
  floor `go 1.26`.
- Server: cap request bodies at 4 KiB.
- Server: cap outstanding challenges (`PBS_MAX_PENDING`, default 100); further
  `/auth/challenge` requests get `503`.
- Server: fix a leaked idle TLS connection per `/auth/verify` in the target-PBS
  probe.
- Client: cap auth-server responses at 64 KiB (the challenge response is read
  before the server proof is verified).
- Client: warn if `secret.key` is readable by group or others.
- systemd units: `NoNewPrivileges=yes`, `PrivateTmp=yes`.

### Added
- Server logs every auth result with the client IP (`auth ok for …`,
  `auth denied for …: <reason>`).
- `PBS_TRUSTED_PROXIES`: CIDRs/IPs of reverse proxies whose `X-Forwarded-For` /
  `X-Real-IP` is trusted for that client IP (rightmost non-trusted hop; logging
  only, never used for authentication). Unset = headers ignored.
- Reverse-proxy examples: per-IP rate limits (Traefik, nginx, HAProxy),
  listener-wide for Envoy, documented options for Caddy and Apache; commented IP
  allowlists; client IP forwarded via `X-Forwarded-For`.
- CI runs `go test -race` and `govulncheck`, plus a weekly scheduled scan;
  Dependabot for Go modules, the Docker base image and GitHub Actions.

### Changed
- Server `WriteTimeout` derives from `PBS_TARGET_TIMEOUT`, so a slow target
  probe no longer cuts off the `/auth/verify` response.
- Bumped GitHub Actions and the example proxy images (Traefik v3.7, nginx 1.30,
  HAProxy 3.4 LTS, Envoy v1.40).

### Upgrade notes
- Server: pull the new image. Behind a reverse proxy, set `PBS_TRUSTED_PROXIES`
  to the proxy's network, otherwise the proxy's address is logged.
- Client: `apt update && apt upgrade` (the running daemon is restarted).

## [0.0.5] - 2026-08-19

### Changed
- Client: log lines for skipped cycles state that the auth check succeeded
  (`pbs-sync-auth ok, but backup not due: …`).

### Fixed
- CI: re-running a tag build no longer fails when the GitHub release already
  exists (which also skipped the APT repository deploy).

## [0.0.4] - 2026-08-19

### Fixed
- `.deb` upgrade from the timer-based layout keeps the offsite push running: an
  enabled timer is migrated to the new daemon service, and a running daemon is
  restarted on upgrade.

## [0.0.3] - 2026-08-19

### Added
- Server: optional target-PBS reachability gate (`PBS_TARGET_URL`,
  `PBS_TARGET_TIMEOUT`); `/auth/verify` reports `target_unavailable` after
  successful auth when the target PBS does not answer.
- Client runs as a daemon (systemd service) with `PBS_CHECK_INTERVAL` and
  `PBS_MIN_BACKUP_INTERVAL`, skips while a sync is already running, and supports
  `--once` with exit codes 0/1/2/3. Configuration in the conffile
  `/etc/pbs-sync-auth/client.conf`.
- Signed APT repository on GitHub Pages
  (`apt install pbs-sync-auth-client`, updates via `apt upgrade`).
- Reverse-proxy examples for nginx, Caddy, HAProxy, Apache httpd and Envoy.
- Installation section in the README.

### Changed
- The `.deb` replaces the systemd timer with the daemon service.
- Release artifacts (binaries, `.deb`, GitHub release) are built only for
  `vX.Y.Z` tags; superseded `main` runs are cancelled.
- GitHub Actions bumped to Node 24 runtimes.

## [0.0.2] - 2026-08-18

### Added
- Debian package for the client (amd64, PBS 4.x / Debian 13).
- Handshake test script (`scripts/test-client-auth.sh`).

### Changed
- Client is built for amd64 only (PBS is x86_64-only).

## [0.0.1] - 2026-08-18

### Added
- Initial release: HMAC-SHA256 mutual challenge-response auth server (Docker)
  and client that triggers the PBS push sync job on success.
- Client TLS support (`https://` auth URL, `PBS_AUTH_TLS_VERIFY`,
  `PBS_AUTH_TLS_CA`).
- Multi-arch server image (amd64, arm64, arm/v7) and static binaries via
  GitHub Actions.
- Traefik + Let's Encrypt example (HTTP-01 and Cloudflare DNS-01).
- PBS source and target setup guides.

[Unreleased]: https://github.com/ftaeger/pbs-sync-auth/compare/v1.0.1...HEAD
[1.0.1]: https://github.com/ftaeger/pbs-sync-auth/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/ftaeger/pbs-sync-auth/compare/v0.0.5...v1.0.0
[0.0.5]: https://github.com/ftaeger/pbs-sync-auth/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/ftaeger/pbs-sync-auth/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/ftaeger/pbs-sync-auth/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/ftaeger/pbs-sync-auth/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/ftaeger/pbs-sync-auth/releases/tag/v0.0.1
