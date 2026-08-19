// Challenge-response auth client (static binary running on the source PBS).
//
// Runs as a long-lived daemon: on a configurable interval it uses mutual HMAC
// authentication to check whether the auth server is reachable AND genuine, then
// (only if a backup is due and none of its own runs is in progress) starts the
// push sync job. The server may additionally report that the target PBS is
// unavailable, in which case the sync is skipped and the reason is logged.
//
// With --once it runs a single cycle and exits, for testing/scripting:
//
//	Exit 0  -> synced, or skipped (backup not yet due, or a sync already running)
//	Exit 1  -> auth server unreachable / auth failed (sync skipped)
//	Exit 2  -> auth ok, but `sync-job run` failed
//	Exit 3  -> auth ok, but the target PBS is unavailable (sync skipped)
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
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// authResult is the outcome of a successful auth round: the server's status
// ("ok" or "target_unavailable") and an optional human-readable message.
type authResult struct {
	status  string
	message string
}

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

func parseDurationOr(s string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
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

// authenticate performs the mutual HMAC round. A returned error means the server
// was unreachable or authentication failed (client or server side); otherwise the
// authResult carries the server's application-level status.
func authenticate() (authResult, error) {
	secret, err := loadSecret(env("PBS_AUTH_SECRET", "/etc/pbs-sync-auth/secret.key"))
	if err != nil {
		return authResult{}, fmt.Errorf("secret: %w", err)
	}
	base := env("PBS_AUTH_URL", "http://pbs-sync-auth.example.com:8099")
	timeout := parseDurationOr(env("PBS_AUTH_TIMEOUT", "3s"), 3*time.Second)
	client, err := buildHTTPClient(timeout)
	if err != nil {
		return authResult{}, err
	}

	nb := make([]byte, 16)
	if _, err := rand.Read(nb); err != nil {
		return authResult{}, fmt.Errorf("rand: %w", err)
	}
	cn := hex.EncodeToString(nb) // fresh client nonce -> replay protection

	var ch struct {
		ServerNonce string `json:"server_nonce"`
		ServerProof string `json:"server_proof"`
	}
	if err := post(client, base+"/auth/challenge", map[string]string{"client_nonce": cn}, &ch); err != nil {
		return authResult{}, fmt.Errorf("challenge: %w", err)
	}

	// 1) Authenticate the server: only the genuine server knows the secret.
	if !hmac.Equal([]byte(ch.ServerProof), []byte(mac(secret, "server", cn, ch.ServerNonce))) {
		return authResult{}, fmt.Errorf("server authentication failed (wrong peer?)")
	}

	// 2) Authenticate ourselves to the server (401 -> post returns an error).
	var res struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := post(client, base+"/auth/verify", map[string]string{
		"client_nonce": cn,
		"server_nonce": ch.ServerNonce,
		"client_proof": mac(secret, "client", cn, ch.ServerNonce),
	}, &res); err != nil {
		return authResult{}, fmt.Errorf("verify: %w", err)
	}
	return authResult{status: res.Status, message: res.Message}, nil
}

// backupDue reports whether a sync should run given the last successful sync time.
// minInterval <= 0 means "always"; a missing last run also counts as due (fail-open).
func backupDue(last time.Time, haveLast bool, minInterval time.Duration, now time.Time) (bool, string) {
	if minInterval <= 0 {
		return true, ""
	}
	if !haveLast {
		return true, ""
	}
	if elapsed := now.Sub(last); elapsed < minInterval {
		return false, fmt.Sprintf("last successful sync %s ago (< %s)", elapsed.Round(time.Second), minInterval)
	}
	return true, ""
}

// syncJobMatches reports whether a task belongs to the given sync job. PBS
// reports the job under worker_type "syncjob" with a worker_id that may be a
// compound like "remote:store:localstore::jobid"; we accept either the whole id
// or its trailing (last colon-separated) segment.
func syncJobMatches(workerType, workerID, job string) bool {
	if workerType != "syncjob" || workerID == "" {
		return false
	}
	if workerID == job {
		return true
	}
	parts := strings.Split(workerID, ":")
	return parts[len(parts)-1] == job
}

// parseLastSuccessfulSync finds the most recent successful sync-job task for the
// given job in the JSON output of `proxmox-backup-manager task list`. It reads the
// worker_type/worker_id/status/endtime fields directly (running tasks carry no
// status/endtime and are ignored). Validated against real PBS 4.x output.
func parseLastSuccessfulSync(data []byte, job string) (time.Time, bool, error) {
	var tasks []struct {
		Status     string `json:"status"`
		EndTime    int64  `json:"endtime"`
		WorkerType string `json:"worker_type"`
		WorkerID   string `json:"worker_id"`
	}
	if err := json.Unmarshal(data, &tasks); err != nil {
		return time.Time{}, false, err
	}
	var best int64
	found := false
	for _, tk := range tasks {
		if tk.Status != "OK" || tk.EndTime <= 0 {
			continue
		}
		if !syncJobMatches(tk.WorkerType, tk.WorkerID, job) {
			continue
		}
		if tk.EndTime > best {
			best, found = tk.EndTime, true
		}
	}
	if !found {
		return time.Time{}, false, nil
	}
	return time.Unix(best, 0), true, nil
}

// taskListJSON returns the raw JSON of the PBS task list. Command/flags validated
// against a real PBS 4.x.
func taskListJSON() ([]byte, error) {
	return exec.Command("proxmox-backup-manager", "task", "list", "--all", "--output-format", "json").Output()
}

// lastSuccessfulSync asks PBS for the last successful run of the sync job.
func lastSuccessfulSync(job string) (time.Time, bool, error) {
	out, err := taskListJSON()
	if err != nil {
		return time.Time{}, false, err
	}
	return parseLastSuccessfulSync(out, job)
}

// syncJobRunning reports whether a sync-job task for the job is currently in
// progress. A running task has no endtime.
func syncJobRunning(data []byte, job string) (bool, error) {
	var tasks []struct {
		EndTime    int64  `json:"endtime"`
		WorkerType string `json:"worker_type"`
		WorkerID   string `json:"worker_id"`
	}
	if err := json.Unmarshal(data, &tasks); err != nil {
		return false, err
	}
	for _, tk := range tasks {
		if tk.EndTime == 0 && syncJobMatches(tk.WorkerType, tk.WorkerID, job) {
			return true, nil
		}
	}
	return false, nil
}

// syncJobIsRunning checks PBS for an in-progress sync of the job (catches runs
// started outside this daemon, e.g. manually or by the PBS scheduler).
func syncJobIsRunning(job string) (bool, error) {
	out, err := taskListJSON()
	if err != nil {
		return false, err
	}
	return syncJobRunning(out, job)
}

// runSyncJob triggers the PBS push sync job and blocks until it finishes.
func runSyncJob(job string) error {
	cmd := exec.Command("proxmox-backup-manager", "sync-job", "run", job)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type cycleOutcome int

const (
	outcomeSynced cycleOutcome = iota
	outcomeSkippedNotDue
	outcomeSkippedRunning
	outcomeTargetUnavailable
	outcomeAuthFailed
	outcomeSyncFailed
)

// cycleDeps holds the (injectable) dependencies of a single cycle so the control
// flow can be tested without a network or a PBS.
type cycleDeps struct {
	authenticate func() (authResult, error)
	lastSync     func(job string) (time.Time, bool, error)
	isRunning    func(job string) (bool, error)
	runSync      func(job string) error
	now          func() time.Time
	job          string
	minBackup    time.Duration
}

// runCycle performs one authenticate -> gate -> maybe-sync cycle.
func runCycle(d cycleDeps) cycleOutcome {
	res, err := d.authenticate()
	if err != nil {
		log.Printf("auth failed / server not reachable: %v (sync skipped)", err)
		return outcomeAuthFailed
	}
	if res.status != "ok" {
		reason := res.message
		if reason == "" {
			reason = res.status
		}
		log.Printf("central side not ready: %s (sync skipped)", reason)
		return outcomeTargetUnavailable
	}

	// Skip if a sync for this job is already running (also catches runs started
	// outside this daemon). Fail-open on error so backups are not blocked.
	if running, rerr := d.isRunning(d.job); rerr != nil {
		log.Printf("warning: could not check for a running sync (%v); proceeding", rerr)
	} else if running {
		log.Printf("a sync job for %s is already running (sync skipped)", d.job)
		return outcomeSkippedRunning
	}

	last, have, lerr := d.lastSync(d.job)
	if lerr != nil {
		log.Printf("warning: could not determine last sync (%v); proceeding", lerr)
		have = false
	}
	if due, reason := backupDue(last, have, d.minBackup, d.now()); !due {
		log.Printf("backup not due: %s (sync skipped)", reason)
		return outcomeSkippedNotDue
	}

	log.Printf("auth ok - starting sync job %s", d.job)
	if err := d.runSync(d.job); err != nil {
		log.Printf("sync-job run failed: %v", err)
		return outcomeSyncFailed
	}
	log.Printf("sync job %s finished", d.job)
	return outcomeSynced
}

func exitCodeFor(o cycleOutcome) int {
	switch o {
	case outcomeAuthFailed:
		return 1
	case outcomeSyncFailed:
		return 2
	case outcomeTargetUnavailable:
		return 3
	default: // outcomeSynced, outcomeSkippedNotDue
		return 0
	}
}

func main() {
	once := flag.Bool("once", false, "run a single cycle and exit (for testing/scripting)")
	flag.Parse()

	job := env("PBS_SYNC_JOB", "offsite-push")
	minBackup := parseDurationOr(env("PBS_MIN_BACKUP_INTERVAL", "0"), 0)
	deps := cycleDeps{
		authenticate: authenticate,
		lastSync:     lastSuccessfulSync,
		isRunning:    syncJobIsRunning,
		runSync:      runSyncJob,
		now:          time.Now,
		job:          job,
		minBackup:    minBackup,
	}

	if *once {
		os.Exit(exitCodeFor(runCycle(deps)))
	}

	checkInterval := parseDurationOr(env("PBS_CHECK_INTERVAL", "30m"), 30*time.Minute)
	if checkInterval <= 0 {
		checkInterval = 30 * time.Minute
	}
	log.Printf("pbs-auth-client daemon: check every %s, min backup interval %s, job %s",
		checkInterval, minBackup, job)

	runCycle(deps) // run immediately on start
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	for {
		select {
		case <-ticker.C:
			runCycle(deps)
		case <-stop:
			log.Printf("shutting down")
			return
		}
	}
}
