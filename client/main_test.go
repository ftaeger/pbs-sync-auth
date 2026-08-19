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
	// UPID:node:pid:pstart:starttime:worktype:workerid:authid:
	data := []byte(`[
	  {"upid":"UPID:pbs:1:1:6531A2B0:syncjob:offsite-push:root@pam:","status":"OK","endtime":1000},
	  {"upid":"UPID:pbs:1:1:6531A2B0:syncjob:offsite-push:root@pam:","status":"OK","endtime":3000},
	  {"upid":"UPID:pbs:1:1:6531A2B0:syncjob:other-job:root@pam:","status":"OK","endtime":9000},
	  {"upid":"UPID:pbs:1:1:6531A2B0:garbage:offsite-push:root@pam:","status":"OK","endtime":9000},
	  {"upid":"UPID:pbs:1:1:6531A2B0:syncjob:offsite-push:root@pam:","status":"failed","endtime":9000}
	]`)

	t.Run("picks latest OK for the job", func(t *testing.T) {
		ts, have, err := parseLastSuccessfulSync(data, "offsite-push")
		if err != nil || !have {
			t.Fatalf("have=%v err=%v", have, err)
		}
		if ts.Unix() != 3000 {
			t.Fatalf("want endtime 3000, got %d", ts.Unix())
		}
	})

	t.Run("unknown job -> no result", func(t *testing.T) {
		_, have, err := parseLastSuccessfulSync(data, "nope")
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if have {
			t.Fatal("want no result")
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
