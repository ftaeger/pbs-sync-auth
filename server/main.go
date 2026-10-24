// Challenge-response auth server (runs as a Docker container on the target side).
//
// Counterpart to the PBS client. Proves via HMAC-SHA256 that both sides hold
// the same shared secret -> mutual authentication.
// Standard library only, no external dependencies.
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	secret       []byte
	pending      = map[string]pendingEntry{}
	pendingMu    sync.Mutex
	challengeTTL = 30 * time.Second

	// Abuse limits: /auth/challenge is unauthenticated, so cap both the request
	// body and the number of outstanding challenges (memory is bounded even under
	// a flood). Each client holds one challenge for at most challengeTTL, so a
	// handful of source PBS needs only a few entries; PBS_MAX_PENDING overrides.
	maxBodyBytes int64 = 4 << 10
	maxPending         = 100

	// Reverse proxies whose X-Forwarded-For / X-Real-IP headers are trusted for
	// logging the real client address (PBS_TRUSTED_PROXIES). Empty = headers
	// ignored, the TCP peer address is logged.
	trustedProxies []netip.Prefix

	// Optional target-PBS reachability gate. If targetURL is empty the feature is
	// disabled and /auth/verify behaves as before (auth only).
	targetURL     string
	targetTimeout = 3 * time.Second
)

type pendingEntry struct {
	clientNonce string
	expiry      time.Time
}

// Domain separation via 'tag' prevents reflection (a server_proof cannot be
// reused as a client_proof).
func mac(tag, cn, sn string) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(tag + "|" + cn + "|" + sn))
	return hex.EncodeToString(h.Sum(nil))
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, obj any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(obj)
}

// parsePrefixes parses a comma-separated list of CIDRs or single IPs.
func parsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			a, err := netip.ParseAddr(f)
			if err != nil {
				return nil, err
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, err
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func isTrusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range trustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// clientIP returns the address to log for a request. Forwarding headers are only
// honored when the TCP peer is a trusted proxy; X-Forwarded-For is then walked
// right to left and the first non-trusted hop is taken, so entries a client
// prepends itself cannot spoof the result. X-Real-IP is the fallback.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !isTrusted(peer) {
		return host
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break // garbage from an untrusted party: stop here
		}
		if !isTrusted(a) {
			return a.Unmap().String()
		}
	}
	if a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		return a.Unmap().String()
	}
	return host
}

func gc() {
	now := time.Now()
	for k, v := range pending {
		if v.expiry.Before(now) {
			delete(pending, k)
		}
	}
}

func challengeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method"})
		return
	}
	var req struct {
		ClientNonce string `json:"client_nonce"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	cn := req.ClientNonce
	if l := len(cn); l < 16 || l > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad client_nonce"})
		return
	}
	sn := randHex(16)
	pendingMu.Lock()
	gc()
	if len(pending) >= maxPending {
		pendingMu.Unlock()
		log.Printf("challenge from %s rejected: %d challenges pending (PBS_MAX_PENDING)", clientIP(r), maxPending)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "busy"})
		return
	}
	pending[sn] = pendingEntry{clientNonce: cn, expiry: time.Now().Add(challengeTTL)}
	pendingMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{
		"server_nonce": sn,
		"server_proof": mac("server", cn, sn),
	})
}

func verifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method"})
		return
	}
	var req struct {
		ClientNonce string `json:"client_nonce"`
		ServerNonce string `json:"server_nonce"`
		ClientProof string `json:"client_proof"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	pendingMu.Lock()
	gc()
	entry, ok := pending[req.ServerNonce]
	if ok {
		delete(pending, req.ServerNonce) // single use only
	}
	pendingMu.Unlock()
	if !ok || entry.clientNonce != req.ClientNonce {
		log.Printf("auth denied for %s: unknown or expired challenge", clientIP(r))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "denied"})
		return
	}
	expected := mac("client", req.ClientNonce, req.ServerNonce)
	if !hmac.Equal([]byte(expected), []byte(req.ClientProof)) {
		log.Printf("auth denied for %s: invalid client proof", clientIP(r))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "denied"})
		return
	}
	// Client is authenticated. Only now (never for unauthenticated peers) do we
	// reveal whether the target PBS is reachable.
	status, message := verifyOutcome(targetURL, func() (bool, string) {
		return targetReachable(targetURL, targetTimeout)
	})
	if message != "" {
		log.Printf("auth ok for %s, %s", clientIP(r), message)
	} else {
		log.Printf("auth ok for %s", clientIP(r))
	}
	resp := map[string]string{"status": status}
	if message != "" {
		resp["message"] = message
	}
	writeJSON(w, http.StatusOK, resp)
}

// targetReachable probes the target PBS API for liveness: an HTTPS GET to
// <base>/api2/json/version with certificate verification disabled (PBS is
// self-signed; this only checks that the API answers — the real push transfer is
// separately fingerprint-pinned). Any HTTP response with status < 500 counts as
// reachable (401 without a token still proves the API is alive). Keep-alives are
// disabled because the transport is per-call: pooled idle connections would
// otherwise never be closed and leak one TLS connection per probe.
func targetReachable(base string, timeout time.Duration) (bool, string) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec // liveness probe only
			DisableKeepAlives: true,
		},
	}
	resp, err := client.Get(strings.TrimRight(base, "/") + "/api2/json/version")
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return false, fmt.Sprintf("http %d", resp.StatusCode)
	}
	return true, ""
}

// verifyOutcome decides the post-auth response. With no target configured the
// client is always told "ok"; otherwise the target must be reachable, and if not,
// the client gets "target_unavailable" plus a message it can log.
func verifyOutcome(targetURL string, probe func() (bool, string)) (status, message string) {
	if targetURL == "" {
		return "ok", ""
	}
	if ok, reason := probe(); !ok {
		return "target_unavailable", fmt.Sprintf("target PBS %s not reachable: %s", targetURL, reason)
	}
	return "ok", ""
}

// healthHandler is a liveness endpoint for reverse-proxy health checks
// (e.g. Traefik). It performs no authentication and returns no sensitive data.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func loadSecret(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimSpace(string(data)))
}

// positiveIntOr parses s as an integer > 0, falling back to def otherwise.
func positiveIntOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	var err error
	secret, err = loadSecret(env("PBS_AUTH_SECRET", "/etc/pbs-sync-auth/secret.key"))
	if err != nil {
		log.Fatalf("loading secret failed: %v", err)
	}
	addr := env("PBS_AUTH_HOST", "0.0.0.0") + ":" + env("PBS_AUTH_PORT", "8099")
	maxPending = positiveIntOr(os.Getenv("PBS_MAX_PENDING"), maxPending)
	if trustedProxies, err = parsePrefixes(os.Getenv("PBS_TRUSTED_PROXIES")); err != nil {
		log.Fatalf("PBS_TRUSTED_PROXIES: %v", err)
	}
	if len(trustedProxies) > 0 {
		log.Printf("trusting forwarding headers from %v", trustedProxies)
	}

	// Optional target-PBS reachability gate.
	targetURL = os.Getenv("PBS_TARGET_URL")
	if d, derr := time.ParseDuration(env("PBS_TARGET_TIMEOUT", "3s")); derr == nil {
		targetTimeout = d
	}
	if targetURL != "" {
		log.Printf("target-PBS reachability gate enabled: %s (timeout %s)", targetURL, targetTimeout)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/challenge", challengeHandler)
	mux.HandleFunc("/auth/verify", verifyHandler)
	mux.HandleFunc("/healthz", healthHandler)

	// /auth/verify may wait for the target probe, so the write deadline must
	// outlast PBS_TARGET_TIMEOUT or a slow probe would cut the response.
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      targetTimeout + 5*time.Second,
		ReadHeaderTimeout: 3 * time.Second,
	}
	log.Printf("pbs-auth-server listening on %s (max %d pending challenges)", addr, maxPending)
	log.Fatal(srv.ListenAndServe())
}
