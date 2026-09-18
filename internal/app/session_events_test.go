package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

type sessionEventFixture struct {
	persistenceFixture
	focused bool
	events  []experience.Event
	notice  string
}

func (f *sessionEventFixture) OwnsKeyboard() bool    { return f.focused }
func (f *sessionEventFixture) Notify(message string) { f.notice = message }
func (f *sessionEventFixture) Handle(e experience.Event) bool {
	f.events = append(f.events, e)
	return true
}

func TestSessionSaveShortcutRespectsApplicationKeyboardOwnership(t *testing.T) {
	work := &sessionEventFixture{focused: true}
	event := experience.Event{Kind: experience.KeyInput, Key: experience.KeyS, Modifiers: experience.ModControl, Pressed: true}
	var output bytes.Buffer
	calls := 0
	save := func() error {
		calls++
		if len(work.events) != 1 || work.events[0].Kind != experience.PointerCancel {
			t.Fatal("session save did not cancel unfinished gesture first")
		}
		return nil
	}
	if quit, err := dispatchEvent(work, event, "workspace.json", &output, save); quit || err != nil {
		t.Fatal(quit, err)
	}
	if calls != 0 || len(work.events) != 1 || work.events[0] != event || work.notice != "" {
		t.Fatal("workspace intercepted focused app's save shortcut")
	}
	work.focused, work.events = false, nil
	if quit, err := dispatchEvent(work, event, "workspace.json", &output, save); quit || err != nil || calls != 1 {
		t.Fatal(quit, err, calls)
	}
	if work.notice != "Workspace saved." || !strings.Contains(output.String(), "saved: workspace.json") {
		t.Fatal("successful save was not reported")
	}
}

func TestSessionSaveFailureKeepsWorkspaceRunning(t *testing.T) {
	work := &sessionEventFixture{}
	var output bytes.Buffer
	errDisk := errors.New("disk unavailable")
	quit, err := dispatchEvent(work, experience.Event{Kind: experience.KeyInput, Key: experience.KeyS, Modifiers: experience.ModControl, Pressed: true}, "workspace.json", &output, func() error { return errDisk })
	if quit || err != nil || !strings.Contains(work.notice, errDisk.Error()) || !strings.Contains(output.String(), "save failed:") {
		t.Fatalf("save failure should stay open and report error: quit=%v err=%v notice=%q output=%q", quit, err, work.notice, output.String())
	}
}
