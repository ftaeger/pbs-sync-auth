# Design: signed APT repository on GitHub Pages

## Goal
Let a source PBS install the client via `apt` and receive updates through
`apt upgrade`, by publishing the client `.deb` in a signed APT repository hosted
on GitHub Pages.

## Approach
Stateless, derived from GitHub Releases. On each `vX.Y.Z` tag (and on manual
`workflow_dispatch`), a CI job rebuilds the whole repository from **all** release
`.deb` assets, signs it, and deploys it to Pages. The GitHub Releases remain the
source of truth; the APT repo is a rebuildable artifact, so no `.deb` blobs or
reprepro database land in git history.

## Hosting
Repository is public; Pages source is "GitHub Actions". Served at
`https://ftaeger.github.io/pbs-sync-auth/`.

## Signing
Dedicated, passphraseless **ed25519** key. Private key in the Actions secret
`GPG_PRIVATE_KEY`; fingerprint in the Actions variable `GPG_KEY_FPR`; public key
committed at `packaging/apt/pubkey.asc` and served at the Pages root. Signing runs
in the GitHub-hosted runner (Model 1). Trade-off: whoever holds the secret can
sign packages — mitigated by a dedicated key and least-privilege job permissions.

## Repository metadata
`reprepro`, single suite/component/arch: `Suite`/`Codename` = `stable`,
`Component` = `main`, `Architectures` = `amd64`. Generic naming keeps it
independent of the Debian version.

## Components
- `packaging/apt/distributions` — reprepro config template (`SignWith: @FPR@`).
- `packaging/apt/pubkey.asc` — public signing key.
- `packaging/apt/index.html` — human landing page (install snippet, fingerprint).
- `packaging/build-apt-repo.sh` — `<deb-dir> <out-dir> <fpr>` → clean signed tree
  (`dists/` + `pool/` + `pubkey.asc` + `index.html`), no `conf/`/`db/`.
- `build.yml` `apt` job — import key, `gh release download` all `*.deb`, build,
  `upload-pages-artifact` + `deploy-pages`. Runs after `release` on tags, or on
  `workflow_dispatch`.

## PBS-side usage
```
curl -fsSL https://ftaeger.github.io/pbs-sync-auth/pubkey.asc \
  | gpg --dearmor | sudo tee /usr/share/keyrings/pbs-sync-auth.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/pbs-sync-auth.gpg] \
  https://ftaeger.github.io/pbs-sync-auth stable main" \
  | sudo tee /etc/apt/sources.list.d/pbs-sync-auth.list
sudo apt update && sudo apt install pbs-sync-auth-client
```

## Verification
After the first deploy, fetch the live `dists/stable/InRelease`, verify its
signature against `pubkey.asc`, and confirm `Packages` lists
`pbs-sync-auth-client`.
