//go:build linux

package app

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSessionLockExclusiveUntilClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "workspace.json")
	first, err := lockSession(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := lockSession(path); err == nil {
		second.Close()
		t.Fatal("two writers acquired the same session")
	}
	before, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0600 {
		t.Fatalf("lock permissions: %v", before.Mode())
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := lockSession(path)
	if err != nil {
		t.Fatalf("released session remained locked: %v", err)
	}
	defer second.Close()
	after, err := second.Stat()
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("lock inode was replaced", err)
	}
	if file, err := lockSession(""); file != nil || err != nil {
		t.Fatal("session without persistence needs no lock", err)
	}
}

func TestSessionLockRejectsSymlinksAndSpecialFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace.json")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(filepath.Join(t.TempDir(), "target"), path+".lock")
			case "fifo":
				err = unix.Mkfifo(path+".lock", 0600)
			case "directory":
				err = os.Mkdir(path+".lock", 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if lock, err := lockSession(path); err == nil {
				lock.Close()
				t.Fatal("accepted non-regular lock")
			}
		})
	}
}

func TestFreshStateRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	work := &persistenceFixture{value: 42}
	if _, _, _, err := loadRecoverableState(path, work, false); err == nil || work.value != 42 {
		t.Fatal("fresh startup accepted a FIFO or changed the document", err)
	}
}
