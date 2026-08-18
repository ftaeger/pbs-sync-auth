#!/usr/bin/env bash
#
# test-client-auth.sh — client-side handshake tester for pbs-sync-auth.
#
# Replays exactly what the Go client does against the auth server, but step by
# step and WITHOUT triggering the real `proxmox-backup-manager sync-job run`.
# Use it to verify from the source PBS (or any client host) that:
#   - the server is reachable,
#   - TLS works (for https:// URLs),
#   - the shared secret matches on both sides (mutual HMAC authentication).
#
# It honours the same environment variables as the Go client:
#   PBS_AUTH_SECRET      path to the 32-byte hex secret   (default: /etc/pbs-sync-auth/secret.key)
#   PBS_AUTH_URL         base URL of the auth server       (default: http://127.0.0.1:8099)
#   PBS_AUTH_TLS_VERIFY  verify server certificate         (default: true; set false for self-signed)
#   PBS_AUTH_TLS_CA      optional CA bundle to trust       (default: system roots)
#   PBS_AUTH_TIMEOUT     per-request timeout, Go duration  (default: 3s)
#
# Runs on Linux and macOS: portable bash (no bash-4-only or GNU-only features),
# using only curl, openssl, awk, sed, tr, tail — identical on both platforms.
#
# Dependencies: curl, openssl (both present on a standard PBS host).
#
# Exit codes:
#   0  all checks passed (server genuine AND client authorized)
#   1  a check failed (unreachable / TLS / secret mismatch / unexpected response)
#   2  usage or missing-dependency error

set -euo pipefail

# ---- configuration (mirrors the Go client) ---------------------------------
SECRET_PATH="${PBS_AUTH_SECRET:-/etc/pbs-sync-auth/secret.key}"
BASE_URL="${PBS_AUTH_URL:-http://127.0.0.1:8099}"
TLS_VERIFY="${PBS_AUTH_TLS_VERIFY:-true}"
TLS_CA="${PBS_AUTH_TLS_CA:-}"
TIMEOUT_RAW="${PBS_AUTH_TIMEOUT:-3s}"

# ---- pretty output ---------------------------------------------------------
if [ -t 1 ]; then
	C_RESET=$'\033[0m'; C_GREEN=$'\033[32m'; C_RED=$'\033[31m'
	C_YELLOW=$'\033[33m'; C_BLUE=$'\033[34m'; C_DIM=$'\033[2m'; C_BOLD=$'\033[1m'
else
	C_RESET=""; C_GREEN=""; C_RED=""; C_YELLOW=""; C_BLUE=""; C_DIM=""; C_BOLD=""
fi

step()  { printf '\n%s==>%s %s%s%s\n' "$C_BLUE" "$C_RESET" "$C_BOLD" "$1" "$C_RESET"; }
ok()    { printf '  %s✔%s %s\n' "$C_GREEN" "$C_RESET" "$1"; }
info()  { printf '  %s·%s %s\n' "$C_DIM" "$C_RESET" "$1"; }
warn()  { printf '  %s!%s %s\n' "$C_YELLOW" "$C_RESET" "$1"; }
fail()  { printf '  %s�’%s %s\n' "$C_RED" "$C_RESET" "$1" >&2; exit 1; }
# err() reports an error but does NOT abort the script (non-fatal).
err()   { printf '  %s�’%s %s\n' "$C_RED" "$C_RESET" "$1" >&2; }

# Set to 1 by the TLS step if the certificate is not valid; reported at the end
# but deliberately does not change the exit code (cert validity is informational).
TLS_CERT_INVALID=0

# ---- helpers ---------------------------------------------------------------

# Convert a Go-style duration (e.g. "3s", "1500ms", "2") to whole curl seconds.
timeout_seconds() {
	local raw="$1"
	case "$raw" in
		*ms) awk -v n="${raw%ms}" 'BEGIN{ s=int((n+999)/1000); print (s<1?1:s) }' ;;
		*s)  awk -v n="${raw%s}"  'BEGIN{ s=int(n+0.999); print (s<1?1:s) }' ;;
		*)   awk -v n="$raw"      'BEGIN{ s=int(n+0.999); print (s<1?1:s) }' ;;
	esac
}

# Extract a string field from a flat JSON object without jq.
json_field() {
	# $1 = field name, reads JSON from stdin
	sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"
}

# HMAC-SHA256 over "$tag|$cn|$sn" with the hex secret, hex-encoded lowercase.
mac() {
	local tag="$1" cn="$2" sn="$3"
	printf '%s|%s|%s' "$tag" "$cn" "$sn" \
		| openssl dgst -sha256 -mac HMAC -macopt "hexkey:$SECRET_HEX" -r \
		| cut -d' ' -f1
}

# curl wrapper: prints "<http_code> <body>" and applies TLS flags. Never aborts
# the script itself on transport errors — we want to report them nicely.
CURL_TLS_ARGS=()
http_post() {
	# $1 = path, $2 = json body
	set +e
	local out
	out=$(curl -sS -m "$TIMEOUT_SECS" ${CURL_TLS_ARGS[@]+"${CURL_TLS_ARGS[@]}"} \
		-o - -w '\n%{http_code}' \
		-X POST -H 'Content-Type: application/json' \
		-d "$2" "$BASE_URL$1" 2>&1)
	local rc=$?
	set -e
	if [ $rc -ne 0 ]; then
		printf 'CURLERR %s' "$out"
		return
	fi
	# last line = status code, everything before = body
	local code body
	code=$(printf '%s' "$out" | tail -n1)
	body=$(printf '%s' "$out" | sed '$d')
	printf '%s\t%s' "$code" "$body"
}

http_get() {
	set +e
	local out
	out=$(curl -sS -m "$TIMEOUT_SECS" ${CURL_TLS_ARGS[@]+"${CURL_TLS_ARGS[@]}"} \
		-o - -w '\n%{http_code}' "$BASE_URL$1" 2>&1)
	local rc=$?
	set -e
	if [ $rc -ne 0 ]; then
		printf 'CURLERR %s' "$out"
		return
	fi
	local code body
	code=$(printf '%s' "$out" | tail -n1)
	body=$(printf '%s' "$out" | sed '$d')
	printf '%s\t%s' "$code" "$body"
}

# ===========================================================================
printf '%spbs-sync-auth — client-side handshake test%s\n' "$C_BOLD" "$C_RESET"
printf '%starget: %s%s\n' "$C_DIM" "$BASE_URL" "$C_RESET"

# --- Step 0: dependencies & config -----------------------------------------
step "Step 0 — dependencies & configuration"
command -v curl >/dev/null 2>&1    || { printf 'missing dependency: curl\n' >&2; exit 2; }
command -v openssl >/dev/null 2>&1 || { printf 'missing dependency: openssl\n' >&2; exit 2; }
ok "curl and openssl present ($(openssl version 2>/dev/null || echo openssl))"

# Self-test the HMAC path against a known vector so an incompatible openssl
# syntax (old/exotic build) fails loudly here instead of as a "secret mismatch".
SELFTEST=$(printf '%s' 'pbs-sync-auth-selftest' \
	| openssl dgst -sha256 -mac HMAC -macopt hexkey:deadbeef -r 2>/dev/null | cut -d' ' -f1 || true)
if [ "$SELFTEST" = "deec9b9c3b14e38c9d88c80de01df62410c38688dc946f96e8fb68cfbb064809" ]; then
	ok "openssl HMAC-SHA256 self-test passed (platform crypto works)"
else
	fail "openssl HMAC self-test failed — this openssl cannot compute the required HMAC (got: ${SELFTEST:-<empty>})"
fi

TIMEOUT_SECS=$(timeout_seconds "$TIMEOUT_RAW")
info "per-request timeout: ${TIMEOUT_SECS}s (from PBS_AUTH_TIMEOUT=${TIMEOUT_RAW})"

IS_HTTPS=0
case "$BASE_URL" in
	https://*)
		IS_HTTPS=1
		# Derive host:port for the standalone certificate probe (Step 2).
		_hp=${BASE_URL#https://}; _hp=${_hp%%/*}
		TLS_HOST=${_hp%%:*}
		case "$_hp" in *:*) TLS_PORT=${_hp##*:} ;; *) TLS_PORT=443 ;; esac
		if [ "$TLS_VERIFY" = "false" ] || [ "$TLS_VERIFY" = "0" ]; then
			CURL_TLS_ARGS+=(-k)
			warn "TLS certificate verification DISABLED (PBS_AUTH_TLS_VERIFY=$TLS_VERIFY) — test/dev only"
		elif [ -n "$TLS_CA" ]; then
			[ -r "$TLS_CA" ] || fail "PBS_AUTH_TLS_CA not readable: $TLS_CA"
			CURL_TLS_ARGS+=(--cacert "$TLS_CA")
			info "trusting custom CA bundle: $TLS_CA"
		else
			info "verifying server certificate against system roots"
		fi
		;;
	http://*)
		info "plain HTTP — no TLS gate (fine for an internal/reverse-proxied setup)"
		;;
	*)
		fail "PBS_AUTH_URL must start with http:// or https:// (got: $BASE_URL)"
		;;
esac

# --- Step 1: load & validate the shared secret -----------------------------
step "Step 1 — shared secret"
[ -r "$SECRET_PATH" ] || fail "secret not readable: $SECRET_PATH (set PBS_AUTH_SECRET)"
SECRET_HEX=$(tr -d '[:space:]' < "$SECRET_PATH")
case "$SECRET_HEX" in
	*[!0-9A-Fa-f]*) fail "secret is not valid hex: $SECRET_PATH" ;;
esac
[ "${#SECRET_HEX}" -eq 64 ] || fail "secret must be 32 bytes (64 hex chars), got ${#SECRET_HEX} chars"
ok "loaded 32-byte hex secret from $SECRET_PATH"

# --- Step 2: TLS certificate validity (https only, non-fatal) --------------
step "Step 2 — TLS certificate"
if [ "$IS_HTTPS" -ne 1 ]; then
	info "skipped — plain HTTP URL, no certificate to check"
else
	# Force certificate verification here regardless of PBS_AUTH_TLS_VERIFY, so an
	# invalid cert is reported even when verification is disabled for the handshake.
	# Honour PBS_AUTH_TLS_CA the same way the Go client does (custom CA replaces roots).
	VERIFY_ARGS=()
	[ -n "$TLS_CA" ] && VERIFY_ARGS+=(--cacert "$TLS_CA")

	set +e
	CERT_ERR=$(curl -sS -o /dev/null -m "$TIMEOUT_SECS" \
		${VERIFY_ARGS[@]+"${VERIFY_ARGS[@]}"} "$BASE_URL/healthz" 2>&1)
	CERT_RC=$?
	set -e
	CERT_ERR=$(printf '%s' "$CERT_ERR" | head -n1)   # keep the message to one line

	# Best-effort certificate details (subject / issuer / expiry) for context.
	CERT_RAW=$(printf '' | openssl s_client -connect "$TLS_HOST:$TLS_PORT" \
		-servername "$TLS_HOST" 2>/dev/null || true)
	CERT_INFO=$(printf '%s\n' "$CERT_RAW" \
		| sed -n '/-----BEGIN CERTIFICATE-----/,/-----END CERTIFICATE-----/p' \
		| openssl x509 -noout -subject -issuer -enddate 2>/dev/null || true)

	case "$CERT_RC" in
		0)
			ok "server certificate is valid (trusted chain, hostname and validity period ok)"
			;;
		60|51|35)
			TLS_CERT_INVALID=1
			err "server certificate is NOT valid: ${CERT_ERR#curl: }"
			warn "reported as an error, continuing anyway (this check does not abort the test)"
			;;
		7|28|6)
			warn "could not probe certificate (server unreachable / timeout): ${CERT_ERR#curl: }"
			;;
		*)
			warn "could not determine certificate validity (curl exit $CERT_RC): ${CERT_ERR#curl: }"
			;;
	esac
	if [ -n "$CERT_INFO" ]; then
		printf '%s\n' "$CERT_INFO" | while IFS= read -r line; do info "$line"; done
	fi
fi

# --- Step 3: liveness (/healthz) -------------------------------------------
step "Step 3 — server reachable (GET /healthz)"
IFS=$'\t' read -r H_CODE H_BODY <<<"$(http_get /healthz)" || true
case "$H_CODE" in
	CURLERR*) fail "cannot reach server: ${H_BODY:-$H_CODE}" ;;
	200)      ok "server is up (HTTP 200: ${H_BODY//$'\n'/ })" ;;
	*)        fail "unexpected /healthz response: HTTP $H_CODE" ;;
esac

# --- Step 4: challenge -> authenticate the SERVER --------------------------
step "Step 4 — challenge: is the server genuine?"
CN=$(openssl rand -hex 16)
info "client_nonce = $CN"
IFS=$'\t' read -r C_CODE C_BODY <<<"$(http_post /auth/challenge "{\"client_nonce\":\"$CN\"}")" || true
case "$C_CODE" in
	CURLERR*) fail "challenge request failed: ${C_BODY:-$C_CODE}" ;;
	200)      : ;;
	*)        fail "challenge returned HTTP $C_CODE — body: $C_BODY" ;;
esac

SN=$(printf '%s' "$C_BODY" | json_field server_nonce)
SPROOF=$(printf '%s' "$C_BODY" | json_field server_proof)
[ -n "$SN" ] && [ -n "$SPROOF" ] || fail "malformed challenge response: $C_BODY"
info "server_nonce  = $SN"

EXP_SPROOF=$(mac server "$CN" "$SN")
if [ "$SPROOF" = "$EXP_SPROOF" ]; then
	ok "server_proof matches our HMAC → server holds the SAME secret (server is genuine)"
else
	printf '    expected: %s\n    received: %s\n' "$EXP_SPROOF" "$SPROOF" >&2
	fail "server_proof MISMATCH → secret differs between client and server (or wrong peer)"
fi

# --- Step 5: verify -> authenticate OURSELVES ------------------------------
step "Step 5 — verify: does the server accept us?"
CPROOF=$(mac client "$CN" "$SN")
V_BODY_JSON="{\"client_nonce\":\"$CN\",\"server_nonce\":\"$SN\",\"client_proof\":\"$CPROOF\"}"
IFS=$'\t' read -r V_CODE V_BODY <<<"$(http_post /auth/verify "$V_BODY_JSON")" || true
case "$V_CODE" in
	CURLERR*) fail "verify request failed: ${V_BODY:-$V_CODE}" ;;
	200)
		STATUS=$(printf '%s' "$V_BODY" | json_field status)
		[ "$STATUS" = "ok" ] || fail "verify returned 200 but status=$STATUS"
		ok "server accepted our client_proof → we are authorized (status: ok)"
		;;
	401) fail "server rejected client_proof (HTTP 401) — unexpected, secret matched in step 4?" ;;
	*)   fail "verify returned HTTP $V_CODE — body: $V_BODY" ;;
esac

# --- Step 6: negative check — single-use challenge -------------------------
step "Step 6 — replay protection (challenge is single-use)"
IFS=$'\t' read -r R_CODE R_BODY <<<"$(http_post /auth/verify "$V_BODY_JSON")" || true
case "$R_CODE" in
	401) ok "replaying the same server_nonce is rejected (HTTP 401) — single-use TTL works" ;;
	CURLERR*) warn "could not run replay check: ${R_BODY:-$R_CODE}" ;;
	*)   warn "replay was NOT rejected (HTTP $R_CODE) — expected 401; check server version" ;;
esac

# ---------------------------------------------------------------------------
printf '\n%s%s✔ ALL GOOD — mutual authentication succeeded.%s\n' "$C_BOLD" "$C_GREEN" "$C_RESET"
printf '%sThe real client would now run: proxmox-backup-manager sync-job run <job>%s\n' "$C_DIM" "$C_RESET"
if [ "$TLS_CERT_INVALID" -eq 1 ]; then
	printf '%s%sNote: the server certificate was reported invalid in step 2 (see above).%s\n' \
		"$C_BOLD" "$C_YELLOW" "$C_RESET"
fi
