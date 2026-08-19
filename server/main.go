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
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	secret       []byte
	pending      = map[string]pendingEntry{}
	pendingMu    sync.Mutex
	challengeTTL = 30 * time.Second

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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "denied"})
		return
	}
	expected := mac("client", req.ClientNonce, req.ServerNonce)
	if !hmac.Equal([]byte(expected), []byte(req.ClientProof)) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "denied"})
		return
	}
	// Client is authenticated. Only now (never for unauthenticated peers) do we
	// reveal whether the target PBS is reachable.
	status, message := verifyOutcome(targetURL, func() (bool, string) {
		return targetReachable(targetURL, targetTimeout)
	})
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
// reachable (401 without a token still proves the API is alive).
func targetReachable(base string, timeout time.Duration) (bool, string) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec // liveness probe only
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

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		ReadHeaderTimeout: 3 * time.Second,
	}
	log.Printf("pbs-auth-server listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}
