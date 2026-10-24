# Contributing

Thanks for your interest in pbs-sync-auth! Bug reports, fixes and improvements
are welcome. By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

**Security issues:** please do not open a public issue — see [SECURITY.md](SECURITY.md).

## Ground rules

- **Go standard library only.** Do not add external module dependencies.
- **Preserve the security properties** of the protocol (fresh nonces on both
  sides, `server`/`client` domain separation, single-use challenges with a TTL,
  constant-time comparison). TLS is an *additional* gate, never a replacement
  for the HMAC handshake.
- Keep it **generic**: no organization-, host- or site-specific references; use
  placeholders such as `pbs-sync-auth.example.com`, `DATASTORE`, `NAMESPACE`.
- Code, comments and docs are in **English**.
- **Never** commit a shared secret (`secret.key` is git-ignored).

## Development

Requires Go (see `go.mod` for the minimum version). Both programs are
standalone modules in `server/` and `client/`:

    cd server && gofmt -l . && go vet ./... && go test -race ./...
    cd client && gofmt -l . && go vet ./... && go test -race ./...

A local end-to-end handshake:

    openssl rand -hex 32 > secret.key
    PBS_AUTH_SECRET=./secret.key PBS_AUTH_PORT=8099 go run ./server &
    PBS_AUTH_SECRET=./secret.key PBS_AUTH_URL=http://127.0.0.1:8099 go run ./client

CI runs the same checks plus `govulncheck` and a `.deb` build on every pull
request; all of them must pass before merging.

## Pull requests

- Branch off `main`, keep each PR focused on one change.
- Add or update tests for behavior changes.
- Add an entry under `## [Unreleased]` in [CHANGELOG.md](CHANGELOG.md)
  (Keep a Changelog: Added / Changed / Fixed / Security …).
- Update the docs (`README.md`, `docs/`, component READMEs) when behavior or
  configuration changes.
- GitHub Actions must be pinned to a full commit SHA with a `# vX.Y.Z` comment;
  workflow permissions stay minimal per job.

PRs are squash-merged.

## Releases (maintainers)

1. On `main`, move the `[Unreleased]` entries into a new dated `## [X.Y.Z]`
   section in `CHANGELOG.md` (plus its compare link) via a PR.
2. Tag the merged commit `vX.Y.Z` and push the tag. The `build.yml` workflow
   builds the image (`:X.Y.Z`, `:X.Y`, `:X`, `:latest`), binaries and `.deb`,
   creates the GitHub release from the changelog section and updates the APT
   repository.
