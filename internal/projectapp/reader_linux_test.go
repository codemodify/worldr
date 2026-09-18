//go:build linux

package projectapp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

func fixtureReader(t *testing.T, root string) projectReader {
	t.Helper()
	r, _, err := openReader(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.close() })
	return r
}

func putFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func readPath(r projectReader, path string, directory bool) result {
	return r.read(request{ctx: context.Background(), path: path, directory: directory})
}

func TestReaderKeepsRootAnchorAndRejectsLinksSpecialFilesAndTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(root, "source.txt"), []byte("anchored text\n"))
	putFile(t, filepath.Join(root, "nested", "note.txt"), []byte("nested text\n"))
	putFile(t, filepath.Join(parent, "outside.txt"), []byte("outside text\n"))
	for link, target := range map[string]string{"file-link": "source.txt", "dir-link": "nested", "outside-link": "../outside.txt"} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	r := fixtureReader(t, root)
	listing := readPath(r, "", true)
	if listing.err != nil || len(listing.entries) != 6 || listing.entries[0].name != "nested" || listing.entries[0].kind != directoryEntry {
		t.Fatalf("directory listing lost sorting/classification: %+v, %v", listing.entries, listing.err)
	}
	kinds := make(map[string]entryKind)
	for _, entry := range listing.entries {
		kinds[entry.name] = entry.kind
	}
	if kinds["file-link"] != symlinkEntry || kinds["dir-link"] != symlinkEntry || kinds["outside-link"] != symlinkEntry || kinds["fifo"] != specialEntry {
		t.Fatalf("links or special files were misclassified: %v", kinds)
	}
	for _, path := range []string{"../outside.txt", "/etc/passwd", "nested/../source.txt", "nested//note.txt", "file-link", "dir-link/note.txt", "outside-link", "fifo"} {
		if res := readPath(r, path, false); res.err == nil {
			t.Fatalf("preview accepted disallowed path %q: %+v", path, res)
		}
	}
	if res := readPath(r, "nested/note.txt", false); res.err != nil || res.preview.text != "nested text\n" {
		t.Fatal("ordinary nested preview failed", res.err)
	}
	// Replacement of the pathname after startup cannot move this reader to
	// another project. Its open directory descriptor remains the root.
	if err := os.Rename(root, filepath.Join(parent, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(root, "source.txt"), []byte("replacement text\n"))
	if res := readPath(r, "source.txt", false); res.err != nil || res.preview.text != "anchored text\n" {
		t.Fatal("root replacement changed the reader's anchor", res.err)
	}
}

func TestReaderAllowsExplicitStartupSymlinkButNotReplacedChild(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "actual")
	if err := os.MkdirAll(filepath.Join(root, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(root, "source.txt"), []byte("chosen project"))
	link := filepath.Join(parent, "chosen")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	r, absolute, err := openReader(link)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if absolute != root || readPath(r, "source.txt", false).preview.text != "chosen project" {
		t.Fatal("explicit startup symlink was not resolved")
	}
	if err := os.Rename(filepath.Join(root, "child"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(root, "child")); err != nil {
		t.Fatal(err)
	}
	if res := readPath(r, "child", true); res.err == nil {
		t.Fatal("navigation followed a child replaced with a symlink")
	}
}

func TestReaderBoundsTextAndDirectoryWork(t *testing.T) {
	root := t.TempDir()
	r := fixtureReader(t, root)
	for _, test := range []struct {
		name      string
		data      []byte
		wantBytes int
		truncated bool
		rejected  bool
	}{
		{"empty", nil, 0, false, false},
		{"utf8", []byte("λ\treadable\r\n"), len("λ\treadable\r\n"), false, false},
		{"exact limit", bytes.Repeat([]byte{'a'}, maxPreview), maxPreview, false, false},
		{"over limit", bytes.Repeat([]byte{'a'}, maxPreview+20), maxPreview, true, false},
		{"split rune", append(bytes.Repeat([]byte{'a'}, maxPreview-1), []byte("€tail")...), maxPreview - 1, true, false},
		{"binary nul", []byte{'a', 0, 'b'}, 0, false, true},
		{"invalid utf8", []byte{'a', 0xff, 'b'}, 0, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			putFile(t, filepath.Join(root, "fixture"), test.data)
			res := readPath(r, "fixture", false)
			if test.rejected {
				if res.err == nil || res.preview.text != "" {
					t.Fatal("binary preview was accepted")
				}
				return
			}
			if res.err != nil || len(res.preview.text) != test.wantBytes || res.truncated != test.truncated || !utf8.ValidString(res.preview.text) {
				t.Fatalf("bounded preview: bytes=%d, truncated=%t, err=%v", len(res.preview.text), res.truncated, res.err)
			}
		})
	}
	for i := 0; i <= maxEntries; i++ {
		putFile(t, filepath.Join(root, fmt.Sprintf("entry-%04d", i)), nil)
	}
	res := readPath(r, "", true)
	if res.err != nil || len(res.entries) != maxEntries || !res.truncated {
		t.Fatalf("directory work was not bounded: entries=%d, truncated=%t, err=%v", len(res.entries), res.truncated, res.err)
	}
	for i := 1; i < len(res.entries); i++ {
		if strings.ToLower(res.entries[i-1].name) > strings.ToLower(res.entries[i].name) {
			t.Fatal("bounded listing is not sorted")
		}
	}
}

func TestReaderCanceledRequestDoesNotOpenAPath(t *testing.T) {
	r := fixtureReader(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := r.read(request{ctx: ctx, path: "missing.txt"})
	if res.err != context.Canceled {
		t.Fatalf("canceled read attempted filesystem lookup: %v", res.err)
	}
}

func TestMediaReaderTransfersReadOnlyAnchoredFileWithoutTextPreview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "clip.webm")
	contents := []byte{0x1a, 0x45, 0xdf, 0xa3, 0, 0xff, 0x80}
	putFile(t, path, contents)
	r := fixtureReader(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res := r.read(request{ctx: ctx, path: "clip.webm", openMedia: true})
	if res.err != nil || res.file == nil || res.preview.text != "" {
		t.Fatal("media open attempted a UTF-8 preview instead of transferring the file", res.err)
	}
	defer res.close()
	flags, err := unix.FcntlInt(res.file.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		t.Fatal("media descriptor allowed writing to project content", err)
	}
	if err := os.Rename(path, filepath.Join(root, "renamed.webm")); err != nil {
		t.Fatal(err)
	}
	putFile(t, path, []byte("replacement pathname"))
	cancel() // A handed-off descriptor has an independent owner/lifetime.
	data, err := io.ReadAll(res.file)
	if err != nil || !bytes.Equal(data, contents) {
		t.Fatal("rename, replacement or later cancellation changed the anchored media file", err)
	}
}

func TestMediaReaderRejectsLinksReplacedAfterListingAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "clip.mp4")
	putFile(t, path, []byte("original"))
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	putFile(t, outside, []byte("outside project"))
	r := fixtureReader(t, root)
	listing := readPath(r, "", true)
	if listing.err != nil || len(listing.entries) != 1 || listing.entries[0].kind != fileEntry {
		t.Fatal("media fixture was not listed as a regular file", listing.err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "linked-folder")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo.mp4"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder.mp4"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"clip.mp4", "linked-folder/outside.mp4", "fifo.mp4", "folder.mp4", "../outside.mp4", outside} {
		res := r.read(request{ctx: context.Background(), path: path, openMedia: true})
		if res.file != nil {
			res.close()
			t.Fatalf("media reader returned a descriptor for disallowed path %q", path)
		}
		if res.err == nil {
			t.Fatalf("media reader accepted disallowed path %q", path)
		}
	}
}
