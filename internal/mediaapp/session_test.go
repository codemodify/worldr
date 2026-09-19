package mediaapp

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/codemodify/worldr/internal/media"
)

type resumePlaybackFixture struct {
	fakePlayback
	initial      media.InitialState
	loadState    media.State
	loadErr      error
	seekErr      error
	unpauseCalls int
}

func (p *resumePlaybackFixture) Load(*os.File) error { p.loadState = p.state; return p.loadErr }
func (p *resumePlaybackFixture) Seek(position float64) error {
	if p.seekErr != nil {
		return p.seekErr
	}
	p.seeks = append(p.seeks, position)
	return nil // Tests explicitly signal seek completion, independently of Load.
}
func (p *resumePlaybackFixture) Pause(paused bool) error {
	if !paused {
		p.unpauseCalls++
	}
	p.state.Paused = paused
	return nil
}

func newResumeManager(t *testing.T) (*Manager, *resumePlaybackFixture) {
	t.Helper()
	m := NewManager(media.Options{})
	p := &resumePlaybackFixture{}
	m.factory = func(options media.Options) (playback, error) {
		if options.InitialState == nil {
			t.Fatal("restore did not configure initial playback state")
		}
		p.initial = *options.InitialState
		p.state = media.State{Paused: p.initial.Paused, Volume: p.initial.Volume, Muted: p.initial.Muted}
		return p, nil
	}
	t.Cleanup(func() { _ = m.Close() })
	return m, p
}

func TestMediaSessionRestoreGatesPlaybackUntilLoadedSeekCompletes(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "playing", true: "paused"}[paused], func(t *testing.T) {
			m, p := newResumeManager(t)
			file := mediaFile(t)
			saved := SessionState{Path: file.Name(), Position: 42.5, Paused: paused, Volume: 18, Muted: true}
			if _, err := m.RestoreFile(file, "display label only.mp4", saved); err != nil {
				t.Fatal(err)
			}
			if !p.loadState.Paused || !p.loadState.Muted || p.loadState.Volume != 18 || m.focused || len(p.seeks) != 0 {
				t.Fatal("restore loaded with default audio state, sought before metadata, or took focus")
			}
			if got, ok := m.SessionState(); !ok || got != saved {
				t.Fatalf("save during loading lost desired resume state: %+v %v", got, ok)
			}
			if err := m.Poll(); err != nil {
				t.Fatal(err)
			}
			if len(p.seeks) != 0 || p.unpauseCalls != 0 {
				t.Fatal("restore sought or played before the file was loaded")
			}
			p.state.Loaded, p.state.Duration = true, 120
			if err := m.Poll(); err != nil {
				t.Fatal(err)
			}
			if len(p.seeks) != 1 || p.seeks[0] != saved.Position || p.unpauseCalls != 0 {
				t.Fatal("loaded restore did not defer unpausing until its seek completed")
			}
			for i := 0; i < 3; i++ {
				if err := m.Poll(); err != nil {
					t.Fatal(err)
				}
			}
			if p.unpauseCalls != 0 || len(p.seeks) != 1 {
				t.Fatal("pending seek was reissued or resumed early")
			}
			p.state.Position, p.state.SeekRevision = saved.Position, 1
			if err := m.Poll(); err != nil {
				t.Fatal(err)
			}
			if m.resume != nil || p.state.Paused != saved.Paused || m.focused {
				t.Fatal("seek completion lost saved pause state or stole focus")
			}
			if got, ok := m.SessionState(); !ok || got != saved {
				t.Fatalf("completed restore differs from session: %+v %v", got, ok)
			}
			m.CloseApplication(m.next)
			if _, ok := m.SessionState(); ok {
				t.Fatal("closed player remained in session")
			}
			if _, err := file.Stat(); err == nil {
				t.Fatal("restore did not transfer descriptor ownership")
			}
		})
	}
}

func TestMediaSessionTracksDescriptorPathAndBoundsSnapshots(t *testing.T) {
	m, _ := testManager(t)
	original := m.file.Name()
	renamed := filepath.Join(filepath.Dir(original), "renamed\nactual.mp4")
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("different file"), 0600); err != nil {
		t.Fatal(err)
	}
	m.state.Position, m.state.Volume = math.Inf(1), math.NaN()
	saved, ok := m.SessionState()
	if !ok || saved.Path != renamed || saved.Position != 0 || saved.Volume != 70 || saved.Validate() != nil {
		t.Fatalf("descriptor path or finite session bounds lost: %+v %v", saved, ok)
	}
	m.state.Position, m.state.Volume = 2*maxSessionPosition, 101
	saved, ok = m.SessionState()
	if !ok || saved.Position != maxSessionPosition || saved.Volume != 100 {
		t.Fatalf("snapshot escaped supported resume limits: %+v", saved)
	}
	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.SessionState(); ok {
		t.Fatal("unlinked video saved a stale original filename")
	}
}

func TestMediaSessionRestoreValidationAndFailureKeepCurrentPlayer(t *testing.T) {
	m, old := testManager(t)
	id, owned := m.next, m.file
	file := mediaFile(t)
	base := SessionState{Path: file.Name(), Volume: 70}
	invalid := []SessionState{
		{Path: "relative.mp4", Volume: 70},
		{Path: file.Name(), Position: -1, Volume: 70},
		{Path: file.Name(), Position: math.NaN(), Volume: 70},
		{Path: file.Name(), Position: math.Inf(1), Volume: 70},
		{Path: file.Name(), Position: maxSessionPosition + 1, Volume: 70},
		{Path: file.Name(), Volume: -1}, {Path: file.Name(), Volume: 101},
		{Path: file.Name(), Volume: math.NaN()}, {Path: file.Name(), Volume: math.Inf(1)},
	}
	for _, saved := range invalid {
		if _, err := m.RestoreFile(file, "invalid.mp4", saved); err == nil {
			t.Fatalf("accepted invalid restore state %+v", saved)
		}
	}
	replacement := &resumePlaybackFixture{loadErr: errors.New("decode setup failed")}
	m.factory = func(media.Options) (playback, error) { return replacement, nil }
	if _, err := m.RestoreFile(file, "failed.mp4", base); err == nil {
		t.Fatal("failed restore reported success")
	}
	if !replacement.closed || old.closed || m.next != id || m.file != owned {
		t.Fatal("failed restore leaked its decoder or discarded current playback")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("failed restore closed caller-owned file")
	}
}

func TestMediaSessionClampsToDurationAndLeavesFailedSeekPaused(t *testing.T) {
	m, p := newResumeManager(t)
	file := mediaFile(t)
	if _, err := m.RestoreFile(file, "shorter.mp4", SessionState{Path: file.Name(), Position: 90, Volume: 23}); err != nil {
		t.Fatal(err)
	}
	p.state.Loaded, p.state.Duration = true, 12
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	if len(p.seeks) != 1 || p.seeks[0] != 12 || p.unpauseCalls != 0 {
		t.Fatal("restore failed to clamp saved position to the actual file duration")
	}
	failed, decoder := newResumeManager(t)
	other := mediaFile(t)
	if _, err := failed.RestoreFile(other, "unseekable.mp4", SessionState{Path: other.Name(), Position: 10, Volume: 25}); err != nil {
		t.Fatal(err)
	}
	decoder.state.Loaded, decoder.seekErr = true, errors.New("seek unavailable")
	if err := failed.Poll(); err != nil {
		t.Fatal(err)
	}
	if failed.message != "seek unavailable" || !decoder.state.Paused || decoder.unpauseCalls != 0 {
		t.Fatal("failed seek resumed audio or hid its error")
	}
}

func TestMediaSessionPendingRestoreCannotAffectReplacement(t *testing.T) {
	m, old := newResumeManager(t)
	file := mediaFile(t)
	if _, err := m.RestoreFile(file, "old.mp4", SessionState{Path: file.Name(), Position: 40, Volume: 12, Paused: true, Muted: true}); err != nil {
		t.Fatal(err)
	}
	replacement := &fakePlayback{state: media.State{Loaded: true, Duration: 120, Volume: 70}}
	m.factory = func(options media.Options) (playback, error) {
		if options.InitialState != nil {
			t.Fatal("restore changed the manager's normal launch options")
		}
		return replacement, nil
	}
	newFile := mediaFile(t)
	if _, err := m.OpenFile(newFile, "normal.mp4"); err != nil {
		t.Fatal(err)
	}
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	if !old.closed || m.resume != nil || len(replacement.seeks) != 0 || replacement.state.Paused || replacement.state.Muted {
		t.Fatal("stale restore settings reached the replacement player")
	}
	if state, ok := m.SessionState(); !ok || state.Path != newFile.Name() || state.Position != 0 || state.Volume != 70 {
		t.Fatalf("replacement retained the prior resume snapshot: %+v %v", state, ok)
	}
}
