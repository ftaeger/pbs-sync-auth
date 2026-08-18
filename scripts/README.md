# scripts/

Helper scripts for operating and testing pbs-sync-auth.

## `test-client-auth.sh` — client-side handshake tester

Replays exactly what the Go client does against the auth server — reachability,
TLS gate (for `https://`), and the mutual HMAC-SHA256 challenge/response — **but
without** triggering the real `proxmox-backup-manager sync-job run`. Use it from
the source PBS (or any client host) to confirm that the server is genuine and
that the shared secret matches on both sides.

Runs on **Linux and macOS**: portable `bash` (no bash-4-only or GNU-only
features), using only `curl`, `openssl`, `awk`, `sed`, `tr`, `tail` — all present
on a standard PBS host.

### Usage

```bash
PBS_AUTH_SECRET=/etc/pbs-sync-auth/secret.key \
PBS_AUTH_URL=https://pbs-sync-auth.example.com \
./scripts/test-client-auth.sh
```

For a local plain-HTTP server (e.g. the `docker compose` setup on `:8099`):

```bash
PBS_AUTH_SECRET=./secret.key \
PBS_AUTH_URL=http://127.0.0.1:8099 \
./scripts/test-client-auth.sh
```

### Configuration

The script honours the **same environment variables as the Go client**:

| Variable | Default | Meaning |
|---|---|---|
| `PBS_AUTH_SECRET` | `/etc/pbs-sync-auth/secret.key` | Path to the 32-byte hex shared secret. |
| `PBS_AUTH_URL` | `http://127.0.0.1:8099` | Base URL of the auth server (`http://` or `https://`). |
| `PBS_AUTH_TLS_VERIFY` | `true` | For `https://`: verify the server certificate. Set `false` for self-signed (test/dev only). |
| `PBS_AUTH_TLS_CA` | *(system roots)* | Optional CA bundle to trust instead of only the system roots (e.g. an internal CA). |
| `PBS_AUTH_TIMEOUT` | `3s` | Per-request timeout (Go-style duration, e.g. `3s`, `1500ms`). |

### What it checks

| Step | Check |
|---|---|
| 0 | `curl`/`openssl` present, TLS configuration, and an openssl HMAC self-test against a known vector. |
| 1 | Shared secret is readable and valid (32 bytes / 64 hex chars). |
| 2 | **TLS certificate validity** (`https://` only) — see below. Skipped for plain HTTP. |
| 3 | Server is reachable — `GET /healthz` returns `200`. |
| 4 | `POST /auth/challenge`, then re-computes `server_proof` locally → **is the server genuine?** |
| 5 | `POST /auth/verify` with our `client_proof` → **does the server accept us?** |
| 6 | Replays the same `server_nonce` → must be rejected (`401`), proving single-use/replay protection. |

#### TLS certificate check (step 2)

For `https://` URLs the script verifies whether the server presents a **valid**
certificate (trusted chain, matching hostname, within validity period). This
check honours `PBS_AUTH_TLS_CA` (a custom CA is trusted the same way the Go
client does) but is otherwise run **with verification forced on** — so an invalid
certificate is reported even when `PBS_AUTH_TLS_VERIFY=false` disables the gate
for the handshake itself.

An invalid certificate is reported as an **error but is non-fatal**: the script
continues with the handshake and (if the handshake succeeds) still exits `0`,
appending a reminder note at the end. This lets you run against a self-signed or
misconfigured cert and still confirm the HMAC auth works, while being clearly
told the certificate is not valid.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | All checks passed — server genuine **and** client authorized. |
| `1` | A check failed (unreachable / TLS / secret mismatch / unexpected response). |
| `2` | Usage or missing-dependency error. |

### Expected output (success)

```text
pbs-sync-auth — client-side handshake test
target: http://127.0.0.1:8099

==> Step 0 — dependencies & configuration
  ✔ curl and openssl present (OpenSSL 3.x)
  ✔ openssl HMAC-SHA256 self-test passed (platform crypto works)
  · per-request timeout: 3s (from PBS_AUTH_TIMEOUT=3s)
  · plain HTTP — no TLS gate (fine for an internal/reverse-proxied setup)

==> Step 1 — shared secret
  ✔ loaded 32-byte hex secret from /etc/pbs-sync-auth/secret.key

==> Step 2 — TLS certificate
  ✔ server certificate is valid (trusted chain, hostname and validity period ok)
  · subject=CN=pbs-sync-auth.example.com
  · issuer=C=US, O=Let's Encrypt, CN=R3
  · notAfter=Nov 16 12:00:00 2026 GMT

==> Step 3 — server reachable (GET /healthz)
  ✔ server is up (HTTP 200: ok)

==> Step 4 — challenge: is the server genuine?
  · client_nonce = 7f9a2d09a2f736f4e1b7a51dcfa6ce25
  · server_nonce  = 7877639012df1ebd120b674baa60d824
  ✔ server_proof matches our HMAC → server holds the SAME secret (server is genuine)

==> Step 5 — verify: does the server accept us?
  ✔ server accepted our client_proof → we are authorized (status: ok)

==> Step 6 — replay protection (challenge is single-use)
  ✔ replaying the same server_nonce is rejected (HTTP 401) — single-use TTL works

✔ ALL GOOD — mutual authentication succeeded.
The real client would now run: proxmox-backup-manager sync-job run <job>
```

### Common failure cases

**Invalid TLS certificate** (`https://`) — reported as an error but **non-fatal**;
the handshake still runs and, if it succeeds, the script exits `0`:

```text
==> Step 2 — TLS certificate
  �’ server certificate is NOT valid: (60) SSL certificate problem: self signed certificate
  ! reported as an error, continuing anyway (this check does not abort the test)
  · subject=CN=127.0.0.1
  · issuer=CN=127.0.0.1
  · notAfter=Aug 20 17:05:24 2026 GMT
...
✔ ALL GOOD — mutual authentication succeeded.
Note: the server certificate was reported invalid in step 2 (see above).
```

**Wrong/mismatched secret** — the server's proof won't match, so it fails in
step 4 before we ever send our own proof:

```text
==> Step 4 — challenge: is the server genuine?
  · client_nonce = f5d294c7913a3372c7d761ea0ad8b5ba
  · server_nonce  = 56f030017c17bf6a45ea24e66b0c9415
    expected: cd019d81fa7baea64891a3a48ce1787e0c0b6237b507a8764aa7542b9ac23f76
    received: 5361f75ecb3db061ff7b4f17d75237180dffa212f4b438be4fbb5c3b5bc57e46
  �’ server_proof MISMATCH → secret differs between client and server (or wrong peer)
```

**Server unreachable** — fails at the liveness check:

```text
==> Step 3 — server reachable (GET /healthz)
  �’ cannot reach server: CURLERR curl: (7) Failed to connect ...
```

**TLS gate fails during the handshake** (`https://` with an untrusted/self-signed
cert and `PBS_AUTH_TLS_VERIFY=true`) — step 2 reports the invalid certificate
(non-fatal), then the reachability check in step 3 fails fatally because the
handshake cannot proceed securely. Fix it properly by pointing `PBS_AUTH_TLS_CA`
at your CA bundle, or — for test/dev only — set
`PBS_AUTH_TLS_VERIFY=false`.
