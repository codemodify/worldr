package app

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func recoveryPath(path string) string {
	if path == "" {
		return ""
	}
	return path + ".autosave"
}

type autosaveWrite struct {
	data []byte
	hash [sha256.Size]byte
}

type autosaveResult struct {
	hash [sha256.Size]byte
	err  error
}

// autosaver belongs to the host goroutine. Only its worker accesses the
// filesystem; snapshots and completion bookkeeping stay on the host.
type autosaver struct {
	interval         time.Duration
	next             time.Time
	started, pending bool
	closed, hasHash  bool
	lastHash         [sha256.Size]byte
	closeErr         error
	writes           chan autosaveWrite
	results          chan autosaveResult
	done             chan struct{}
}

func newAutosaver(path string, interval time.Duration) *autosaver {
	return newAutosaverWithWriter(path, interval, writeState)
}

func newAutosaverWithWriter(path string, interval time.Duration, write func(string, []byte) error) *autosaver {
	if path == "" || interval <= 0 {
		return nil
	}
	a := &autosaver{interval: interval, writes: make(chan autosaveWrite, 1), results: make(chan autosaveResult, 1), done: make(chan struct{})}
	go func() {
		defer close(a.done)
		for job := range a.writes {
			a.results <- autosaveResult{hash: job.hash, err: write(recoveryPath(path), job.data)}
		}
	}()
	return a
}

// Tick first starts the interval clock, then captures at most one snapshot per
// interval. An outstanding write suppresses captures instead of queuing stale
// snapshots. The next available due tick captures the latest host state.
func (a *autosaver) Tick(now time.Time, snapshot func() ([]byte, error)) error {
	if a == nil || a.closed {
		return nil
	}
	var completedErr error
	if a.pending {
		select {
		case result := <-a.results:
			completedErr = a.complete(result)
		default:
		}
	}
	if !a.started {
		a.started, a.next = true, now.Add(a.interval)
		return completedErr
	}
	if a.pending || now.Before(a.next) {
		return completedErr
	}
	a.next = now.Add(a.interval)
	if snapshot == nil {
		return errors.Join(completedErr, fmt.Errorf("autosave snapshot is unavailable"))
	}
	data, err := snapshot()
	if err != nil {
		return errors.Join(completedErr, fmt.Errorf("autosave snapshot: %w", err))
	}
	if len(data) > maxStateBytes {
		return errors.Join(completedErr, fmt.Errorf("autosave snapshot exceeds %d bytes", maxStateBytes))
	}
	hash := sha256.Sum256(data)
	if a.hasHash && hash == a.lastHash {
		return completedErr
	}
	// Own the bytes even when a snapshot hook returns reusable backing storage.
	a.pending = true
	a.writes <- autosaveWrite{data: bytes.Clone(data), hash: hash}
	return completedErr
}

func (a *autosaver) complete(result autosaveResult) error {
	a.pending = false
	if result.err != nil {
		// A directory-sync failure can occur after replacement. The file may
		// contain the failed snapshot, so even an earlier successful hash must
		// be written again if the user has since returned to that state.
		a.hasHash = false
		return fmt.Errorf("autosave: %w", result.err)
	}
	a.lastHash, a.hasHash = result.hash, true
	return nil
}

// Flush waits only for the current write; it does not capture another snapshot.
func (a *autosaver) Flush() error {
	if a == nil {
		return nil
	}
	if a.closed {
		return a.closeErr
	}
	if a.pending {
		return a.complete(<-a.results)
	}
	return nil
}

// Reset records a successfully completed manual save. The caller must Flush
// first, so an earlier autosave completion cannot overwrite this baseline.
func (a *autosaver) Reset(data []byte) {
	if a != nil && !a.closed {
		a.lastHash, a.hasHash = sha256.Sum256(data), true
	}
}

func (a *autosaver) Close() error {
	if a == nil {
		return nil
	}
	if a.closed {
		return a.closeErr
	}
	a.closeErr = a.Flush()
	a.closed = true
	close(a.writes)
	<-a.done
	return a.closeErr
}

type stateCandidate struct {
	path     string
	recovery bool
	info     os.FileInfo
	err      error
}

func inspectState(path string, recovery bool) stateCandidate {
	info, err := os.Stat(path)
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("state must be a regular file")
	}
	return stateCandidate{path: path, recovery: recovery, info: info, err: err}
}

// recoveryLoad distinguishes a file disappearing between Stat and Open from a
// successfully loaded version-1 file, whose session result is also nil. Envelope
// validation stays in loadSessionState; the experience validates transactionally.
type recoveryLoad struct {
	experience.Experience
	stateful experience.Stateful
	applied  bool
}

func (r *recoveryLoad) SaveState() ([]byte, error) { return r.stateful.SaveState() }
func (r *recoveryLoad) LoadState(data []byte) error {
	if err := r.stateful.LoadState(data); err != nil {
		return err
	}
	r.applied = true
	return nil
}

// loadRecoverableState tries the newest candidate first, falling back to the
// other only after a failed load. Invalid envelopes never reach LoadState, and
// Stateful's transactional contract preserves the live state on invalid payloads.
func loadRecoverableState(path string, exp experience.Experience, recover bool) (session *sessionManifest, recovered bool, notice string, err error) {
	if path == "" || !recover {
		session, err = loadSessionState(path, exp)
		return session, false, "", err
	}
	stateful, err := persistentExperience(exp)
	if err != nil {
		return nil, false, "", err
	}
	main := inspectState(path, false)
	backup := inspectState(recoveryPath(path), true)
	candidates := []stateCandidate{main, backup}
	if !errors.Is(backup.err, os.ErrNotExist) &&
		(backup.info == nil || main.info == nil || backup.info.ModTime().After(main.info.ModTime())) {
		candidates[0], candidates[1] = backup, main
	}
	var failures []error
	for _, candidate := range candidates {
		if errors.Is(candidate.err, os.ErrNotExist) {
			continue
		}
		candidateErr := candidate.err
		var manifest *sessionManifest
		if candidateErr == nil {
			probe := &recoveryLoad{Experience: exp, stateful: stateful}
			manifest, candidateErr = loadSessionState(candidate.path, probe)
			if candidateErr == nil && !probe.applied {
				continue // It disappeared; allow the other candidate or a fresh start.
			}
		}
		if candidateErr != nil {
			name := "saved workspace"
			if candidate.recovery {
				name = "autosave recovery"
			}
			failures = append(failures, fmt.Errorf("%s: %w", name, candidateErr))
			continue
		}
		if candidate.recovery {
			return manifest, true, "Recovered workspace from autosave.", nil
		}
		if len(failures) > 0 {
			return manifest, false, "Autosave recovery was invalid; loaded the saved workspace instead.", nil
		}
		return manifest, false, "", nil
	}
	return nil, false, "", errors.Join(failures...)
}
