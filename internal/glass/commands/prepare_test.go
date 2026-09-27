package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/terminal"
)

func TestPrepareLeavesCustomCommandsAndArgumentsAlone(t *testing.T) {
	for _, options := range []terminal.Options{
		{Command: "/bin/sh"}, {Command: "/bin/zsh"},
		{Command: "/bin/bash", Args: []string{"-l"}},
		{Command: "/bin/bash", Args: []string{"-c", "printf custom"}},
	} {
		prepared, cleanup, integrated, err := Prepare(options)
		if err != nil || integrated || !reflect.DeepEqual(prepared, options) {
			t.Fatalf("changed explicit launch: %+v, %v, %v", prepared, integrated, err)
		}
		cleanup()
		cleanup()
	}
}

func TestPrepareUsesPrivateDisposableRCFile(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash unavailable")
	}
	t.Setenv("SHELL", bash)
	prepared, cleanup, integrated, err := Prepare(terminal.Options{Cols: 90})
	if err != nil || !integrated || prepared.Cols != 90 || len(prepared.Args) != 3 {
		t.Fatalf("prepare = %+v, %v, %v", prepared, integrated, err)
	}
	defer cleanup()
	path := prepared.Args[1]
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private rcfile: %v, %v", info, err)
	}
	info, err = os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private directory: %v, %v", info, err)
	}
	content, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(content), "builtin source -- \"$HOME/.bashrc\"") || !strings.HasSuffix(string(content), terminal.BashIntegration) {
		t.Fatalf("wrong rcfile: %q, %v", content, err)
	}
	cleanup()
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rcfile not removed: %v", err)
	}
}
