//go:build linux && cgo

package nativeapps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func TestRealPTYTaskRequiresAnExplicitEnterAfterStaging(t *testing.T) {
	p, err := New(Options{Command: "/bin/sh", Cols: 80, Rows: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	marker := filepath.Join(t.TempDir(), "task-ran")
	command := "printf ran > '" + marker + "'"
	if err := p.SetTaskRecipes([]TaskRecipe{{Name: "Write marker", Command: command}}); err != nil {
		t.Fatal(err)
	}
	p.Focus(1)
	p.setTool("tasks")
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	for _, pressed := range []bool{true, false} {
		p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 31, Pressed: pressed})
	}
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("staging a task executed it without Enter")
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, pressed := range []bool{true, false} {
		p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: pressed})
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(marker); err == nil {
			if string(data) != "ran" {
				t.Fatalf("task output = %q", data)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real shell did not execute staged task after explicit Enter")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
