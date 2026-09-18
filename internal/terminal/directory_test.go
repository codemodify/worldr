//go:build linux && cgo

package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func directoryFDCount(t *testing.T, pid int, directory os.FileInfo) int {
	t.Helper()
	path := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(path, entry.Name()))
		if err == nil && os.SameFile(info, directory) {
			count++
		}
	}
	return count
}

func TestDirectoryAnchorsPTYAfterRenameAndPathReplacement(t *testing.T) {
	parent, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	original := filepath.Join(root, "selected")
	renamed := filepath.Join(root, "photo's $literal; $(false) `false` directory")
	decoy := filepath.Join(root, "decoy")
	for _, path := range []string{original, decoy} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{original: "ANCHORED_CONTENT", decoy: "WRONG_CONTENT"} {
		if err := os.WriteFile(filepath.Join(path, "specimen"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directory.Close() })
	identity, err := directory.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(decoy, original); err != nil {
		t.Fatal(err)
	}
	// This valid logical alias would be retained by the shell if inherited
	// PWD survived. An anchored launch must let the shell discover its cwd.
	alias := filepath.Join(root, "inherited-pwd-alias")
	if err := os.Symlink(renamed, alias); err != nil {
		t.Fatal(err)
	}
	literal := "$(false); '$HOME' `false`"
	environment := []string{"PATH=" + os.Getenv("PATH"), "PWD=" + alias, "WAYLAND_DISPLAY=/private/worldr-test", "DISPLAY=:private-test", "PRIVATE=retained"}
	before := append([]string(nil), environment...)
	term := openTestTerminal(t, Options{
		Command: "/bin/sh", Args: []string{"-c", `printf 'PWD:<%s>\nENV:<%s>|<%s>|<%s>\nARG:<%s>\nREADY\n' "$PWD" "$WAYLAND_DISPLAY" "$DISPLAY" "$PRIVATE" "$1"; IFS= read -r input; printf 'CONTENT:<'; cat specimen; printf '>\n'`, "fixture", literal},
		Env: environment, Directory: directory, Cols: 512, Rows: 10,
	})
	first := pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "READY") })
	for _, expected := range []string{"PWD:<" + renamed + ">", "ENV:</private/worldr-test>|<:private-test>|<retained>", "ARG:<" + literal + ">"} {
		if !strings.Contains(screenText(first), expected) {
			t.Fatalf("directory, child environment, or literal argument changed: missing %q in %q", expected, screenText(first))
		}
	}
	if !reflect.DeepEqual(environment, before) {
		t.Fatal("anchored launch mutated caller-owned environment")
	}
	childInfo, err := os.Stat(fmt.Sprintf("/proc/%d/cwd", term.cmd.Process.Pid))
	if err != nil || !os.SameFile(identity, childInfo) {
		t.Fatalf("PTY child did not enter the borrowed directory inode: %v", err)
	}
	if current, err := directory.Stat(); err != nil || !os.SameFile(identity, current) {
		t.Fatalf("successful Open closed or replaced the caller's directory: %v", err)
	}
	if count := directoryFDCount(t, term.cmd.Process.Pid, identity); count != 0 {
		t.Fatalf("PTY child inherited %d directory descriptors", count)
	}
	if count := directoryFDCount(t, os.Getpid(), identity); count != 1 {
		t.Fatalf("Open retained a duplicate directory descriptor: got %d", count)
	}
	if err := directory.Close(); err != nil {
		t.Fatal(err)
	}
	if count := directoryFDCount(t, os.Getpid(), identity); count != 0 {
		t.Fatalf("directory remains open after the caller closed it: %d", count)
	}
	current, err := os.Stat(".")
	if err != nil || !os.SameFile(parentInfo, current) {
		t.Fatalf("terminal launch changed the parent's working directory: %v", err)
	}
	if cwd, err := os.Getwd(); err != nil || cwd != parent {
		t.Fatalf("parent cwd changed to %q: %v", cwd, err)
	}
	sibling := openTestTerminal(t, Options{Command: "/bin/pwd", Args: []string{"-P"}, Cols: 512, Rows: 3})
	siblingResult := pollTerminal(t, sibling, func(s Snapshot) bool { return s.Exited })
	if strings.TrimSpace(screenText(siblingResult)) != parent {
		t.Fatalf("nil Directory changed the sibling terminal's cwd: %q", screenText(siblingResult))
	}
	key(t, term, 28, 0)
	last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if !strings.Contains(screenText(last), "CONTENT:<ANCHORED_CONTENT>") || strings.Contains(screenText(last), "WRONG_CONTENT") || last.ExitError != "" {
		t.Fatalf("closing the borrowed descriptor broke the child cwd: %q (%s)", screenText(last), last.ExitError)
	}
}

func TestDirectoryRemovesInheritedPWD(t *testing.T) {
	root := t.TempDir()
	actual, alias := filepath.Join(root, "actual"), filepath.Join(root, "logical-alias")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(actual)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	t.Setenv("PWD", alias)
	t.Setenv("WAYLAND_DISPLAY", "/private/inherited-worldr")
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", `printf 'PWD:<%s>\nWAYLAND:<%s>\n' "$PWD" "$WAYLAND_DISPLAY"`}, Directory: directory, Cols: 512, Rows: 4})
	last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if !strings.Contains(screenText(last), "PWD:<"+actual+">") || !strings.Contains(screenText(last), "WAYLAND:</private/inherited-worldr>") {
		t.Fatalf("inherited environment or directory incorrect: %q", screenText(last))
	}
	if os.Getenv("PWD") != alias {
		t.Fatal("terminal changed the parent's PWD environment")
	}
}

func TestInvalidDirectoryAndSpawnFailurePreserveCallerState(t *testing.T) {
	parent, err := os.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, kind := range []string{"closed", "regular", "spawn"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(root, kind)
			if kind == "regular" {
				err = os.WriteFile(path, nil, 0600)
			} else {
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			identity, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if kind == "closed" {
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			marker := filepath.Join(root, "must-not-start-"+kind)
			options := Options{Command: "/bin/sh", Args: []string{"-c", `printf started > "$1"`, "fixture", marker}, Directory: file}
			if kind == "spawn" {
				options.Command = filepath.Join(root, "missing-executable")
			}
			term, openErr := Open(options)
			if term != nil {
				_ = term.Close()
			}
			if openErr == nil || term != nil {
				t.Fatal("invalid directory or missing executable unexpectedly started a terminal")
			}
			if kind != "spawn" && !strings.Contains(openErr.Error(), "terminal directory") {
				t.Fatalf("directory was not validated before process startup: %v", openErr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("invalid directory allowed the child command to run: %v", err)
			}
			info, err := file.Stat()
			if kind == "closed" {
				if err == nil {
					t.Fatal("Open revived a closed descriptor")
				}
			} else if err != nil || !os.SameFile(identity, info) {
				t.Fatalf("failed Open closed the caller's file: %v", err)
			}
			current, err := os.Stat(".")
			if err != nil || !os.SameFile(parent, current) {
				t.Fatalf("failed Open changed the parent's working directory: %v", err)
			}
			if kind == "spawn" && directoryFDCount(t, os.Getpid(), identity) != 1 {
				t.Fatal("failed process startup leaked the directory descriptor")
			}
		})
	}
}
