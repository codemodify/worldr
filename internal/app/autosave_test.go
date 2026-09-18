package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type autosaveCall struct {
	path string
	data []byte
}

type autosaveWriter struct {
	calls   chan autosaveCall
	results chan error
	stop    chan struct{}
}

func controlledAutosaver(t *testing.T) (*autosaver, *autosaveWriter) {
	t.Helper()
	w := &autosaveWriter{calls: make(chan autosaveCall, 4), results: make(chan error, 4), stop: make(chan struct{})}
	a := newAutosaverWithWriter("/virtual/workspace.json", time.Second, func(path string, data []byte) error {
		w.calls <- autosaveCall{path: path, data: data}
		select {
		case err := <-w.results:
			return err
		case <-w.stop:
			return errors.New("test writer stopped")
		}
	})
	t.Cleanup(func() { close(w.stop); a.Close() })
	return a, w
}

func nextAutosave(t *testing.T, w *autosaveWriter) autosaveCall {
	t.Helper()
	select {
	case call := <-w.calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("autosave worker did not receive the snapshot")
		return autosaveCall{}
	}
}

func TestAutosaveOwnsSnapshotBytesAndCoalescesWhileWorkerIsBlocked(t *testing.T) {
	a, writer := controlledAutosaver(t)
	now := time.Unix(100, 0)
	data, captures := []byte("first"), 0
	snapshot := func() ([]byte, error) { captures++; return data, nil }
	if err := a.Tick(now, snapshot); err != nil || captures != 0 {
		t.Fatal("first tick did not start an interval", err)
	}
	if err := a.Tick(now.Add(time.Second), snapshot); err != nil || captures != 1 {
		t.Fatal("due snapshot was not captured synchronously on the caller", err)
	}
	first := nextAutosave(t, writer)
	data[0] = 'X'
	if string(first.data) != "first" || first.path != "/virtual/workspace.json.autosave" {
		t.Fatal("worker borrowed mutable host storage or wrote the primary path")
	}
	finished := make(chan error, 1)
	go func() {
		for i := 2; i < 50; i++ {
			if err := a.Tick(now.Add(time.Duration(i)*time.Second), snapshot); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("frame ticks blocked on filesystem work")
	}
	if captures != 1 || len(writer.calls) != 0 {
		t.Fatal("blocked writer queued additional snapshots")
	}
	data = []byte("latest")
	writer.results <- nil
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := a.Tick(now.Add(50*time.Second), snapshot); err != nil {
		t.Fatal(err)
	}
	if call := nextAutosave(t, writer); string(call.data) != "latest" || captures != 2 {
		t.Fatal("worker wrote a stale queued snapshot instead of current state")
	}
	writer.results <- nil
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := a.Tick(now.Add(51*time.Second), snapshot); err != nil || a.pending || captures != 3 {
		t.Fatal("identical successful snapshot triggered another write", err)
	}
}

func TestAutosaveRetriesFailedWriteOnNextIntervalEvenForEarlierSuccessfulState(t *testing.T) {
	for _, retry := range []string{"failed", "earlier"} {
		t.Run(retry, func(t *testing.T) {
			a, writer := controlledAutosaver(t)
			now := time.Unix(100, 0)
			data := []byte("earlier")
			snapshot := func() ([]byte, error) { return data, nil }
			a.Tick(now, snapshot)
			a.Tick(now.Add(time.Second), snapshot)
			nextAutosave(t, writer)
			writer.results <- nil
			if err := a.Flush(); err != nil {
				t.Fatal(err)
			}
			data = []byte("failed")
			a.Tick(now.Add(2*time.Second), snapshot)
			nextAutosave(t, writer)
			failure := errors.New("directory sync failed after replacement")
			writer.results <- failure
			deadline := time.Now().Add(time.Second)
			for len(a.results) == 0 {
				if time.Now().After(deadline) {
					t.Fatal("worker did not publish its write failure")
				}
				time.Sleep(time.Millisecond)
			}
			if err := a.Tick(now.Add(2500*time.Millisecond), snapshot); !errors.Is(err, failure) || a.pending {
				t.Fatal("failure was hidden or retried before the next interval", err)
			}
			data = []byte(retry)
			if err := a.Tick(now.Add(3*time.Second), snapshot); err != nil {
				t.Fatal(err)
			}
			if call := nextAutosave(t, writer); string(call.data) != retry {
				t.Fatal("failed write was not retried with the latest snapshot")
			}
			writer.results <- nil
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAutosaveFlushAndResetPreventLateWritesOverManualSaveBaseline(t *testing.T) {
	a, writer := controlledAutosaver(t)
	now := time.Unix(100, 0)
	data := []byte("before manual save")
	snapshot := func() ([]byte, error) { return data, nil }
	a.Tick(now, snapshot)
	a.Tick(now.Add(time.Second), snapshot)
	nextAutosave(t, writer)
	flushed := make(chan error, 1)
	go func() { flushed <- a.Flush() }()
	select {
	case <-flushed:
		t.Fatal("Flush returned before the pending write finished")
	case <-time.After(10 * time.Millisecond):
	}
	writer.results <- nil
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	data = []byte("successful manual save")
	a.Reset(data)
	if err := a.Tick(now.Add(2*time.Second), snapshot); err != nil || a.pending || len(writer.calls) != 0 {
		t.Fatal("stale completion replaced the successful manual-save baseline", err)
	}
	data = []byte("edited after manual save")
	if err := a.Tick(now.Add(3*time.Second), snapshot); err != nil {
		t.Fatal(err)
	}
	if call := nextAutosave(t, writer); string(call.data) != string(data) {
		t.Fatal("new edit was lost after Reset")
	}
	writer.results <- nil
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.done:
	default:
		t.Fatal("Close did not join the worker")
	}
	if err := a.Close(); err != nil {
		t.Fatal("idempotent close failed", err)
	}
	if err := a.Tick(now.Add(4*time.Second), func() ([]byte, error) { t.Fatal("closed worker captured state"); return nil, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestAutosaveDisabledSnapshotFailureAndCloseErrors(t *testing.T) {
	for _, a := range []*autosaver{nil, newAutosaver("", time.Second), newAutosaver("unused", 0), newAutosaver("unused", -time.Second)} {
		if a != nil {
			t.Fatal("disabled autosaver started a worker")
		}
		if err := a.Tick(time.Now(), func() ([]byte, error) { t.Fatal("disabled snapshot invoked"); return nil, nil }); err != nil {
			t.Fatal(err)
		}
		a.Reset(nil)
		if a.Flush() != nil || a.Close() != nil {
			t.Fatal("disabled autosaver was not nil-safe")
		}
	}
	a, writer := controlledAutosaver(t)
	now := time.Unix(100, 0)
	a.Tick(now, nil)
	if err := a.Tick(now.Add(time.Second), nil); err == nil {
		t.Fatal("nil snapshot hook was accepted")
	}
	failure := errors.New("checkpoint failed")
	if err := a.Tick(now.Add(2*time.Second), func() ([]byte, error) { return nil, failure }); !errors.Is(err, failure) || a.pending {
		t.Fatal("snapshot failure was hidden or queued for writing", err)
	}
	if err := a.Tick(now.Add(2500*time.Millisecond), func() ([]byte, error) { t.Fatal("failed capture retried within interval"); return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.Tick(now.Add(3*time.Second), func() ([]byte, error) { return make([]byte, maxStateBytes+1), nil }); err == nil || a.pending {
		t.Fatal("unbounded snapshot was queued")
	}
	a.Tick(now.Add(4*time.Second), func() ([]byte, error) { return []byte("valid snapshot"), nil })
	nextAutosave(t, writer)
	writer.results <- failure
	if err := a.Close(); !errors.Is(err, failure) {
		t.Fatal("Close hid a pending write failure", err)
	}
	if err := a.Close(); !errors.Is(err, failure) {
		t.Fatal("second Close lost its result", err)
	}
}

type autosaveCheckpointFixture struct {
	persistenceFixture
	saves, checkpoints int
}

func (f *autosaveCheckpointFixture) SaveState() ([]byte, error) {
	f.saves++
	return f.persistenceFixture.SaveState()
}
func (f *autosaveCheckpointFixture) CheckpointState() ([]byte, error) {
	f.checkpoints++
	return f.persistenceFixture.SaveState()
}

func TestAutosaveWritesPrivateRecoveryWithoutSavingExperienceOnWorkerOrReplacingPrimary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	primary := []byte("existing manual save")
	if err := os.WriteFile(path, primary, 0600); err != nil {
		t.Fatal(err)
	}
	exp := &autosaveCheckpointFixture{persistenceFixture: persistenceFixture{value: 42}}
	a := newAutosaver(path, time.Second)
	t.Cleanup(func() { a.Close() })
	now := time.Unix(100, 0)
	var captured []byte
	snapshot := func() ([]byte, error) {
		var err error
		captured, err = encodeState(exp, &sessionManifest{Version: 1}, true)
		return captured, err
	}
	a.Tick(now, snapshot)
	if err := a.Tick(now.Add(time.Second), snapshot); err != nil || exp.checkpoints != 1 || exp.saves != 0 {
		t.Fatal("autosave did not capture the non-disruptive checkpoint on Tick", err)
	}
	if err := a.Flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(recoveryPath(path))
	if err != nil || !bytes.Equal(data, captured) {
		t.Fatal("worker did not write the owned envelope", err)
	}
	info, err := os.Stat(recoveryPath(path))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("recovery file was not private", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(data, primary) || exp.saves != 0 {
		t.Fatal("autosave changed the primary or called SaveState", err)
	}
}

type recoveryFixture struct {
	persistenceFixture
	loads int
}

func (f *recoveryFixture) LoadState(data []byte) error {
	f.loads++
	return f.persistenceFixture.LoadState(data)
}

func TestRecoveryChoosesNewestValidStateAndFallsBackWithoutLosingPrimary(t *testing.T) {
	encode := func(value int, session *sessionManifest) []byte {
		t.Helper()
		data, err := encodeState(&persistenceFixture{value: value}, session, false)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	main, backup := encode(10, nil), encode(20, &sessionManifest{Version: 1})
	corrupt := []byte(`{"version":`)
	mismatch := bytes.ReplaceAll(backup, []byte("test.experience"), []byte("different.experience"))
	invalidManifest := []byte(`{"version":2,"experience_id":"test.experience","state":{"value":20},"session":{"version":1,"photo":{"path":"relative"}}}`)
	for _, test := range []struct {
		name             string
		main, backup     []byte
		age              time.Duration
		disabled         bool
		value, loads     int
		recovered, error bool
		notice           string
	}{
		{name: "newer recovery", main: main, backup: backup, age: time.Hour, value: 20, loads: 1, recovered: true, notice: "Recovered"},
		{name: "older recovery", main: main, backup: backup, age: -time.Hour, value: 10, loads: 1},
		{name: "equal timestamp", main: main, backup: backup, value: 10, loads: 1},
		{name: "missing main", backup: backup, value: 20, loads: 1, recovered: true, notice: "Recovered"},
		{name: "both missing", value: 7},
		{name: "corrupt main older recovery", main: corrupt, backup: backup, age: -time.Hour, value: 20, loads: 1, recovered: true, notice: "Recovered"},
		{name: "invalid main payload older recovery", main: encode(-1, nil), backup: backup, age: -time.Hour, value: 20, loads: 2, recovered: true, notice: "Recovered"},
		{name: "corrupt recovery", main: main, backup: corrupt, age: time.Hour, value: 10, loads: 1, notice: "invalid"},
		{name: "mismatched recovery", main: main, backup: mismatch, age: time.Hour, value: 10, loads: 1, notice: "invalid"},
		{name: "oversized recovery", main: main, backup: bytes.Repeat([]byte(" "), maxStateBytes+1), age: time.Hour, value: 10, loads: 1, notice: "invalid"},
		{name: "invalid recovery manifest", main: main, backup: invalidManifest, age: time.Hour, value: 10, loads: 1, notice: "invalid"},
		{name: "invalid recovery payload", main: main, backup: encode(-1, nil), age: time.Hour, value: 10, loads: 2, notice: "invalid"},
		{name: "old corrupt recovery ignored", main: main, backup: corrupt, age: -time.Hour, value: 10, loads: 1},
		{name: "both invalid", main: corrupt, backup: mismatch, age: time.Hour, value: 7, error: true},
		{name: "both invalid payloads", main: encode(-1, nil), backup: encode(-2, nil), age: time.Hour, value: 7, loads: 2, error: true},
		{name: "missing main invalid recovery", backup: corrupt, value: 7, error: true},
		{name: "disabled newer recovery", main: main, backup: backup, age: time.Hour, disabled: true, value: 10, loads: 1},
		{name: "disabled missing main", backup: backup, disabled: true, value: 7},
		{name: "disabled corrupt main", main: corrupt, backup: backup, disabled: true, value: 7, error: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace.json")
			stamp := time.Unix(1700000000, 0)
			for _, file := range []struct {
				path string
				data []byte
				time time.Time
			}{{path, test.main, stamp}, {recoveryPath(path), test.backup, stamp.Add(test.age)}} {
				if file.data == nil {
					continue
				}
				if err := os.WriteFile(file.path, file.data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(file.path, file.time, file.time); err != nil {
					t.Fatal(err)
				}
			}
			exp := &recoveryFixture{persistenceFixture: persistenceFixture{value: 7}}
			session, recovered, notice, err := loadRecoverableState(path, exp, !test.disabled)
			if (err != nil) != test.error || recovered != test.recovered || exp.value != test.value || exp.loads != test.loads {
				t.Fatalf("recovered=%v value=%d loads=%d notice=%q err=%v", recovered, exp.value, exp.loads, notice, err)
			}
			if test.notice == "" && notice != "" || test.notice != "" && !strings.Contains(notice, test.notice) {
				t.Fatalf("unexpected recovery notice %q", notice)
			}
			if recovered && (session == nil || session.Version != 1) {
				t.Fatal("recovered envelope lost its session manifest")
			}
			if test.main != nil {
				current, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(current, test.main) {
					t.Fatal("recovery modified the primary save", err)
				}
			}
		})
	}
}
