// Challenge-response auth client (static binary running on the source PBS).
//
// Uses mutual HMAC authentication to check whether the auth server is both
// reachable AND genuine. Only on success is the push sync job started.
//
// Exit 0  -> auth ok, sync job started
// Exit 1  -> auth server unreachable / auth failed (sync skipped)
// Exit 2  -> auth ok, but `sync-job run` failed
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func mac(secret []byte, tag, cn, sn string) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(tag + "|" + cn + "|" + sn))
	return hex.EncodeToString(h.Sum(nil))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadSecret(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimSpace(string(data)))
}

// buildHTTPClient constructs the HTTP client used for the auth calls. For https
// URLs the TLS handshake is the first gate: it happens on the first request
// (/auth/challenge), so if certificate verification fails, authenticate()
// returns before any HMAC round is performed (TLS-first, then auth).
func buildHTTPClient(timeout time.Duration) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	// PBS_AUTH_TLS_VERIFY=false disables certificate verification (test/dev only).
	if verify, _ := strconv.ParseBool(env("PBS_AUTH_TLS_VERIFY", "true")); !verify {
		tlsCfg.InsecureSkipVerify = true
	}

	// PBS_AUTH_TLS_CA (optional): trust a custom CA bundle (e.g. an internal CA)
	// instead of only the system roots. Unset -> system roots are used.
	if caPath := os.Getenv("PBS_AUTH_TLS_CA"); caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("tls ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("tls ca: no certificates found in %s", caPath)
		}
		tlsCfg.RootCAs = pool
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}, nil
}

func post(client *http.Client, url string, payload any, out any) error {
	body, _ := json.Marshal(payload)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func authenticate() error {
	secret, err := loadSecret(env("PBS_AUTH_SECRET", "/etc/pbs-sync-auth/secret.key"))
	if err != nil {
		return fmt.Errorf("secret: %w", err)
	}
	base := env("PBS_AUTH_URL", "http://pbs-sync-auth.example.com:8099")
	timeout, err := time.ParseDuration(env("PBS_AUTH_TIMEOUT", "3s"))
	if err != nil {
		timeout = 3 * time.Second
	}
	client, err := buildHTTPClient(timeout)
	if err != nil {
		return err
	}

	nb := make([]byte, 16)
	if _, err := rand.Read(nb); err != nil {
		return fmt.Errorf("rand: %w", err)
	}
	cn := hex.EncodeToString(nb) // fresh client nonce -> replay protection

	var ch struct {
		ServerNonce string `json:"server_nonce"`
		ServerProof string `json:"server_proof"`
	}
	if err := post(client, base+"/auth/challenge", map[string]string{"client_nonce": cn}, &ch); err != nil {
		return fmt.Errorf("challenge: %w", err)
	}

	// 1) Authenticate the server: only the genuine server knows the secret
	if !hmac.Equal([]byte(ch.ServerProof), []byte(mac(secret, "server", cn, ch.ServerNonce))) {
		return fmt.Errorf("server authentication failed (wrong peer?)")
	}

	// 2) Authenticate ourselves to the server
	var res struct {
		Status string `json:"status"`
	}
	if err := post(client, base+"/auth/verify", map[string]string{
		"client_nonce": cn,
		"server_nonce": ch.ServerNonce,
		"client_proof": mac(secret, "client", cn, ch.ServerNonce),
	}, &res); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if res.Status != "ok" {
		return fmt.Errorf("client authentication rejected")
	}
	return nil
}

func main() {
	ts := func() string { return time.Now().Format(time.RFC3339) }

	if err := authenticate(); err != nil {
		fmt.Fprintf(os.Stderr, "%s auth failed / auth server not reachable: %v\n", ts(), err)
		os.Exit(1)
	}

	job := env("PBS_SYNC_JOB", "offsite-push")
	fmt.Printf("%s auth ok – starting sync job %s.\n", ts(), job)

	cmd := exec.Command("proxmox-backup-manager", "sync-job", "run", job)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s sync-job run failed: %v\n", ts(), err)
		os.Exit(2)
	}
}
