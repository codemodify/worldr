//go:build linux

package resourcepath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidateRequiresCleanAbsoluteJSONRoundTripPath(t *testing.T) {
	for _, path := range []string{"/", "/not/mounted/currently", "/spaces and 'quotes'/literal $(text) `text`", "/世界/é"} {
		if err := Validate(path); err != nil {
			t.Fatalf("pure validation rejected a valid literal path %q: %v", path, err)
		}
	}
	for _, path := range []string{"", ".", "relative", "https://example.test/file", "/tmp/../file", "/tmp//file", "/tmp/", "/bad\x00path", "/bad\xffpath", "/" + strings.Repeat("a", 4096)} {
		if err := Validate(path); err == nil {
			t.Fatalf("accepted non-round-trippable or noncanonical path %q", path)
		}
	}
}

func TestFromFileFollowsOpenedInodeThroughRenameAndRejectsReplacement(t *testing.T) {
	root := t.TempDir()
	original, renamed := filepath.Join(root, "original"), filepath.Join(root, "renamed")
	if err := os.WriteFile(original, []byte("owned content"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := OpenFile(original)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	identity, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("unrelated original name"), 0600); err != nil {
		t.Fatal(err)
	}
	path, err := FromFile(file)
	if err != nil || path != renamed {
		t.Fatalf("saved stale display name rather than opened inode: %q, %v", path, err)
	}
	reopened, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := reopened.Stat()
	reopened.Close()
	if err != nil || !os.SameFile(identity, info) {
		t.Fatal("recorded path did not reopen the owned inode", err)
	}
	replacement := filepath.Join(root, "replacement")
	if err := os.WriteFile(replacement, []byte("new content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, renamed); err != nil {
		t.Fatal(err)
	}
	if path, err := FromFile(file); err == nil || path != "" {
		t.Fatalf("unlinked open resource was confused with its replacement: %q, %v", path, err)
	}
	if info, err := file.Stat(); err != nil || !os.SameFile(identity, info) {
		t.Fatal("path capture closed or altered its borrowed file", err)
	}
}

func TestFromDirectoryFollowsRenameAndRejectsDeletedResource(t *testing.T) {
	root := t.TempDir()
	original, renamed := filepath.Join(root, "folder"), filepath.Join(root, "renamed folder")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := OpenDirectory(original)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if path, err := FromFile(directory); err != nil || path != renamed {
		t.Fatalf("directory rename was not reflected: %q, %v", path, err)
	}
	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	if path, err := FromFile(directory); err == nil || path != "" {
		t.Fatalf("deleted directory produced a reopenable-looking path: %q, %v", path, err)
	}
	directory.Close()
	if _, err := FromFile(directory); err == nil {
		t.Fatalf("closed file descriptor was accepted: %v", err)
	}
	if _, err := FromFile(nil); err == nil {
		t.Fatal("nil descriptor was accepted")
	}
}

func TestSavedResourceOpenRejectsSymlinksSpecialFilesAndWrongTypes(t *testing.T) {
	root := t.TempDir()
	regular, directory, fifo := filepath.Join(root, "file"), filepath.Join(root, "directory"), filepath.Join(root, "fifo")
	if err := os.WriteFile(regular, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	fileLink, directoryLink := filepath.Join(root, "file-link"), filepath.Join(root, "directory-link")
	if err := os.Symlink(regular, fileLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(directory, directoryLink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{fileLink, directoryLink, directory, fifo} {
		if file, err := OpenFile(path); err == nil {
			file.Close()
			t.Fatalf("opened nonregular or symlink file %q", path)
		}
	}
	for _, path := range []string{fileLink, directoryLink, regular, fifo} {
		if file, err := OpenDirectory(path); err == nil {
			file.Close()
			t.Fatalf("opened nondirectory or symlink directory %q", path)
		}
	}
	// An already-authorized descriptor may have been opened through a symlink;
	// capture its canonical inode path, rather than persisting the link itself.
	file, err := os.Open(fileLink)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if path, err := FromFile(file); err != nil || path != regular {
		t.Fatalf("borrowed symlink-opened descriptor lost its real path: %q, %v", path, err)
	}
	fd, err := unix.Open(fifo, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	special := os.NewFile(uintptr(fd), fifo)
	defer special.Close()
	if _, err := FromFile(special); err == nil {
		t.Fatal("special descriptor was persisted as a reopenable resource")
	}
}
