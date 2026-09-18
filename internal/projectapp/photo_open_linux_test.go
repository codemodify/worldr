//go:build linux

package projectapp

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPhotoProviderHandsOffReadOnlyBinaryFileWithIndependentLifetime(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "photo.PNG")
	contents := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0xff}
	putFile(t, path, contents)
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pollUntil(t, p, func() bool { return p.selected == 0 && strings.Contains(p.message, "Photo file") })
	if p.loadingFile || p.preview.text != "" {
		t.Fatal("photo selection attempted a binary text preview")
	}
	var opened *os.File
	p.SetOpenHandler(func(file *os.File, name string) error {
		if name != "photo.PNG" {
			t.Errorf("photo callback basename = %q", name)
		}
		opened = file
		return nil
	})
	p.openSelected()
	pollUntil(t, p, func() bool { return !p.loadingFile })
	if opened == nil {
		t.Fatal("photo descriptor was not handed to the viewer", p.message)
	}
	defer opened.Close()
	flags, err := unix.FcntlInt(opened.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		t.Fatal("photo viewer received a writable descriptor", err)
	}
	if err := os.Rename(path, filepath.Join(root, "renamed.PNG")); err != nil {
		t.Fatal(err)
	}
	putFile(t, path, []byte("replacement file"))
	p.Close()
	data, err := io.ReadAll(opened)
	if err != nil || !bytes.Equal(data, contents) {
		t.Fatal("path replacement or browser shutdown changed the photo viewer's file", err)
	}
}

func TestPhotoProviderRejectsFileReplacedBySymlinkAfterSelection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "photo.bmp")
	putFile(t, path, []byte{'B', 'M', 0})
	outside := filepath.Join(t.TempDir(), "outside.bmp")
	putFile(t, outside, []byte("outside project"))
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pollUntil(t, p, func() bool { return p.selected == 0 && strings.Contains(p.message, "Photo file") })
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	called := false
	p.SetOpenHandler(func(file *os.File, _ string) error {
		called = true
		_ = file.Close()
		return nil
	})
	p.openSelected()
	pollUntil(t, p, func() bool { return !p.loadingFile })
	if called || p.err != nil || p.message == "" {
		t.Fatal("photo activation followed the replaced symlink or failed to report its error")
	}
}
