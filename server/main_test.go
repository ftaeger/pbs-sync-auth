package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// doChallengeVerify runs a full challenge+verify round against the handlers and
// returns the verify HTTP status code and decoded JSON body.
func doChallengeVerify(t *testing.T, clientProof func(cn, sn string) string) (int, map[string]string) {
	t.Helper()
	// challenge
	cn := randHex(16)
	cr := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"client_nonce": cn})
	challengeHandler(cr, httptest.NewRequest(http.MethodPost, "/auth/challenge", bytes.NewReader(body)))
	if cr.Code != http.StatusOK {
		t.Fatalf("challenge status %d", cr.Code)
	}
	var ch struct {
		ServerNonce string `json:"server_nonce"`
		ServerProof string `json:"server_proof"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &ch)
	// verify
	vr := httptest.NewRecorder()
	vbody, _ := json.Marshal(map[string]string{
		"client_nonce": cn,
		"server_nonce": ch.ServerNonce,
		"client_proof": clientProof(cn, ch.ServerNonce),
	})
	verifyHandler(vr, httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(vbody)))
	var out map[string]string
	_ = json.Unmarshal(vr.Body.Bytes(), &out)
	return vr.Code, out
}

func TestTargetReachable(t *testing.T) {
	t.Run("200 is reachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		ok, reason := targetReachable(srv.URL, time.Second)
		if !ok {
			t.Fatalf("want reachable, got not reachable: %q", reason)
		}
	})

	t.Run("401 counts as reachable (API alive)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		if ok, reason := targetReachable(srv.URL, time.Second); !ok {
			t.Fatalf("401 should be reachable, got: %q", reason)
		}
	})

	t.Run("self-signed TLS is reachable (verify skipped)", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		if ok, reason := targetReachable(srv.URL, time.Second); !ok {
			t.Fatalf("self-signed TLS should be reachable, got: %q", reason)
		}
	})

	t.Run("500 is not reachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		ok, reason := targetReachable(srv.URL, time.Second)
		if ok {
			t.Fatal("500 should not be reachable")
		}
		if reason == "" {
			t.Fatal("want a non-empty reason")
		}
	})

	t.Run("connection refused is not reachable", func(t *testing.T) {
		// Nothing listens here.
		ok, reason := targetReachable("http://127.0.0.1:1", 500*time.Millisecond)
		if ok {
			t.Fatal("closed port should not be reachable")
		}
		if reason == "" {
			t.Fatal("want a non-empty reason")
		}
	})

	t.Run("timeout is not reachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		if ok, _ := targetReachable(srv.URL, 20*time.Millisecond); ok {
			t.Fatal("slow server past timeout should not be reachable")
		}
	})
}

// verifyOutcome is the pure decision used by verifyHandler once the client has
// authenticated: given whether a target is configured and reachable, what status
// and message go back to the client.
func TestVerifyOutcome(t *testing.T) {
	t.Run("no target configured -> ok", func(t *testing.T) {
		status, msg := verifyOutcome("", func() (bool, string) { return false, "unused" })
		if status != "ok" || msg != "" {
			t.Fatalf("got status=%q msg=%q", status, msg)
		}
	})

	t.Run("target reachable -> ok", func(t *testing.T) {
		status, msg := verifyOutcome("https://pbs:8007", func() (bool, string) { return true, "" })
		if status != "ok" || msg != "" {
			t.Fatalf("got status=%q msg=%q", status, msg)
		}
	})

	t.Run("target unreachable -> target_unavailable with message", func(t *testing.T) {
		status, msg := verifyOutcome("https://pbs:8007", func() (bool, string) { return false, "connection refused" })
		if status != "target_unavailable" {
			t.Fatalf("got status=%q", status)
		}
		if !strings.Contains(msg, "pbs:8007") || !strings.Contains(msg, "connection refused") {
			t.Fatalf("message should mention host and reason, got: %q", msg)
		}
	})
}

func TestVerifyHandler(t *testing.T) {
	secret = []byte("test-secret-key")
	good := func(cn, sn string) string { return mac("client", cn, sn) }

	t.Run("wrong proof -> 401 denied", func(t *testing.T) {
		targetURL = ""
		code, out := doChallengeVerify(t, func(cn, sn string) string { return "deadbeef" })
		if code != http.StatusUnauthorized || out["status"] != "denied" {
			t.Fatalf("got code=%d status=%q", code, out["status"])
		}
	})

	t.Run("auth ok, no target -> 200 ok", func(t *testing.T) {
		targetURL = ""
		code, out := doChallengeVerify(t, good)
		if code != http.StatusOK || out["status"] != "ok" {
			t.Fatalf("got code=%d status=%q", code, out["status"])
		}
	})

	t.Run("auth ok, target reachable -> 200 ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		targetURL = srv.URL
		targetTimeout = time.Second
		code, out := doChallengeVerify(t, good)
		if code != http.StatusOK || out["status"] != "ok" {
			t.Fatalf("got code=%d status=%q", code, out["status"])
		}
	})

	t.Run("auth ok, target down -> 200 target_unavailable + message", func(t *testing.T) {
		targetURL = "http://127.0.0.1:1"
		targetTimeout = 300 * time.Millisecond
		code, out := doChallengeVerify(t, good)
		if code != http.StatusOK || out["status"] != "target_unavailable" {
			t.Fatalf("got code=%d status=%q", code, out["status"])
		}
		if out["message"] == "" {
			t.Fatal("want a message the client can log")
		}
	})

	targetURL = "" // reset shared state
}
