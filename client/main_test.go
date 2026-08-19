package main

import (
	"errors"
	"testing"
	"time"
)

func TestBackupDue(t *testing.T) {
	now := time.Unix(100000, 0)
	t.Run("min interval 0 -> always due", func(t *testing.T) {
		if due, _ := backupDue(time.Time{}, false, 0, now); !due {
			t.Fatal("want due")
		}
	})
	t.Run("no previous run -> due (fail-open)", func(t *testing.T) {
		if due, _ := backupDue(time.Time{}, false, time.Hour, now); !due {
			t.Fatal("want due")
		}
	})
	t.Run("older than interval -> due", func(t *testing.T) {
		if due, _ := backupDue(now.Add(-2*time.Hour), true, time.Hour, now); !due {
			t.Fatal("want due")
		}
	})
	t.Run("within interval -> not due with reason", func(t *testing.T) {
		due, reason := backupDue(now.Add(-30*time.Minute), true, time.Hour, now)
		if due {
			t.Fatal("want not due")
		}
		if reason == "" {
			t.Fatal("want a reason")
		}
	})
}

func TestParseLastSuccessfulSync(t *testing.T) {
	// Shape taken from real `proxmox-backup-manager task list --output-format json`
	// output: direct worker_type/worker_id fields, compound worker_id, running
	// tasks without status/endtime, and failed tasks with a non-"OK" status.
	data := []byte(`[
	  {"worker_type":"syncjob","worker_id":"Zentrale:NAS01:elw-backup::offsite-push"},
	  {"endtime":1787126400,"status":"OK","worker_type":"prunejob","worker_id":"elw-backup"},
	  {"endtime":1787124304,"status":"client error (Connect): deadline elapsed","worker_type":"syncjob","worker_id":"Zentrale:NAS01:elw-backup::offsite-push"},
	  {"endtime":1787200000,"status":"OK","worker_type":"syncjob","worker_id":"Zentrale:NAS01:elw-backup::offsite-push"},
	  {"endtime":1787100000,"status":"OK","worker_type":"syncjob","worker_id":"Zentrale:NAS01:elw-backup::offsite-push"},
	  {"endtime":1787300000,"status":"OK","worker_type":"syncjob","worker_id":"Other:store::different-job"},
	  {"starttime":1787128220,"worker_type":"termproxy","worker_id":null}
	]`)

	t.Run("matches by trailing job segment, picks latest OK", func(t *testing.T) {
		ts, have, err := parseLastSuccessfulSync(data, "offsite-push")
		if err != nil || !have {
			t.Fatalf("have=%v err=%v", have, err)
		}
		if ts.Unix() != 1787200000 {
			t.Fatalf("want 1787200000, got %d", ts.Unix())
		}
	})

	t.Run("matches the full compound worker_id too", func(t *testing.T) {
		ts, have, err := parseLastSuccessfulSync(data, "Zentrale:NAS01:elw-backup::offsite-push")
		if err != nil || !have || ts.Unix() != 1787200000 {
			t.Fatalf("have=%v ts=%d err=%v", have, ts.Unix(), err)
		}
	})

	t.Run("running/failed/prunejob are ignored", func(t *testing.T) {
		// The only OK syncjob for offsite-push is 1787200000/1787100000; the
		// running (no status), failed, and prunejob entries must not count.
		ts, _, _ := parseLastSuccessfulSync(data, "offsite-push")
		if ts.Unix() == 1787124304 || ts.Unix() == 1787126400 {
			t.Fatal("failed/prunejob leaked into the result")
		}
	})

	t.Run("unknown job -> no result", func(t *testing.T) {
		_, have, err := parseLastSuccessfulSync(data, "nope")
		if err != nil || have {
			t.Fatalf("have=%v err=%v", have, err)
		}
	})

	t.Run("empty list -> no result", func(t *testing.T) {
		_, have, err := parseLastSuccessfulSync([]byte(`[]`), "offsite-push")
		if err != nil || have {
			t.Fatalf("have=%v err=%v", have, err)
		}
	})
}

func TestRunCycle(t *testing.T) {
	base := cycleDeps{
		now:       func() time.Time { return time.Unix(100000, 0) },
		job:       "offsite-push",
		minBackup: 0,
	}
	okAuth := func() (authResult, error) { return authResult{status: "ok"}, nil }
	neverSynced := func(string) (time.Time, bool, error) { return time.Time{}, false, nil }

	t.Run("auth error -> auth failed, no sync", func(t *testing.T) {
		d := base
		d.authenticate = func() (authResult, error) { return authResult{}, errors.New("boom") }
		d.lastSync = neverSynced
		called := false
		d.runSync = func(string) error { called = true; return nil }
		if got := runCycle(d); got != outcomeAuthFailed {
			t.Fatalf("got %v", got)
		}
		if called {
			t.Fatal("sync must not run on auth failure")
		}
	})

	t.Run("target unavailable -> skip, no sync", func(t *testing.T) {
		d := base
		d.authenticate = func() (authResult, error) {
			return authResult{status: "target_unavailable", message: "down"}, nil
		}
		d.lastSync = neverSynced
		called := false
		d.runSync = func(string) error { called = true; return nil }
		if got := runCycle(d); got != outcomeTargetUnavailable {
			t.Fatalf("got %v", got)
		}
		if called {
			t.Fatal("sync must not run when target unavailable")
		}
	})

	t.Run("ok + not due -> skip", func(t *testing.T) {
		d := base
		d.minBackup = time.Hour
		d.authenticate = okAuth
		d.lastSync = func(string) (time.Time, bool, error) { return time.Unix(100000-600, 0), true, nil }
		called := false
		d.runSync = func(string) error { called = true; return nil }
		if got := runCycle(d); got != outcomeSkippedNotDue {
			t.Fatalf("got %v", got)
		}
		if called {
			t.Fatal("sync must not run when not due")
		}
	})

	t.Run("ok + due -> sync runs", func(t *testing.T) {
		d := base
		d.authenticate = okAuth
		d.lastSync = neverSynced
		called := false
		d.runSync = func(job string) error { called = true; return nil }
		if got := runCycle(d); got != outcomeSynced {
			t.Fatalf("got %v", got)
		}
		if !called {
			t.Fatal("sync should have run")
		}
	})

	t.Run("ok + due + sync fails -> sync failed", func(t *testing.T) {
		d := base
		d.authenticate = okAuth
		d.lastSync = neverSynced
		d.runSync = func(string) error { return errors.New("nope") }
		if got := runCycle(d); got != outcomeSyncFailed {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("lastSync error -> fail-open, sync runs", func(t *testing.T) {
		d := base
		d.minBackup = time.Hour
		d.authenticate = okAuth
		d.lastSync = func(string) (time.Time, bool, error) { return time.Time{}, false, errors.New("parse") }
		called := false
		d.runSync = func(string) error { called = true; return nil }
		if got := runCycle(d); got != outcomeSynced {
			t.Fatalf("got %v", got)
		}
		if !called {
			t.Fatal("fail-open should let the sync run")
		}
	})
}

func TestExitCodeFor(t *testing.T) {
	cases := map[cycleOutcome]int{
		outcomeSynced:            0,
		outcomeSkippedNotDue:     0,
		outcomeAuthFailed:        1,
		outcomeSyncFailed:        2,
		outcomeTargetUnavailable: 3,
	}
	for outcome, want := range cases {
		if got := exitCodeFor(outcome); got != want {
			t.Fatalf("outcome %v: want %d got %d", outcome, want, got)
		}
	}
}
