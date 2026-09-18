//go:build linux

package projectapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalHereDirectoryRemainsAnchoredAcrossPathReplacement(t *testing.T) {
	for _, replacement := range []string{"root-before-open", "selected-before-callback"} {
		t.Run(replacement, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "project")
			selected := filepath.Join(root, "folder")
			if err := os.MkdirAll(selected, 0700); err != nil {
				t.Fatal(err)
			}
			putFile(t, filepath.Join(selected, "anchored.txt"), []byte("original folder"))
			p, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { p.Close() })
			pollUntil(t, p, func() bool { return p.selected == 0 })
			replacePath := func(path string) {
				t.Helper()
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(selected, 0700); err != nil {
					t.Fatal(err)
				}
				putFile(t, filepath.Join(selected, "replacement.txt"), []byte("wrong folder"))
			}
			if replacement == "root-before-open" {
				replacePath(root)
			}
			var borrowed *os.File
			p.SetTerminalHandler(func(directory *os.File, displayPath string) error {
				borrowed = directory
				entries, err := directory.ReadDir(-1)
				if err != nil || len(entries) != 1 || entries[0].Name() != "anchored.txt" {
					t.Fatalf("borrowed directory followed replacement path: entries=%v err=%v", entries, err)
				}
				if displayPath != selected {
					t.Fatalf("unexpected informational path %q", displayPath)
				}
				return nil
			})
			p.terminalHere()
			await(t, func() bool { return len(p.results) == 1 })
			if replacement == "selected-before-callback" {
				replacePath(selected)
			}
			if err := p.Poll(); err != nil || borrowed == nil {
				t.Fatal("terminal callback did not receive anchored directory", err, p.notice)
			}
			assertClosedFile(t, borrowed)
			if p.directory != "" || p.selected != 0 || p.entries[0].name != "folder" {
				t.Fatal("launch changed the browser navigation")
			}
		})
	}
}

func TestTerminalHereUsesCurrentNestedFolderForRegularFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(root, "src", "main.go"), []byte("package main"))
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	pollUntil(t, p, func() bool { return p.selected == 0 })
	p.openSelected()
	pollUntil(t, p, func() bool { return p.directory == "src" && p.selected == 0 && !p.loadingDirectory && !p.loadingFile })
	called := false
	p.SetTerminalHandler(func(directory *os.File, displayPath string) error {
		called = true
		names, err := directory.Readdirnames(-1)
		if err != nil || len(names) != 1 || names[0] != "main.go" || displayPath != filepath.Join(root, "src") {
			t.Fatalf("wrong current directory: names=%v display=%q err=%v", names, displayPath, err)
		}
		return nil
	})
	p.terminalHere()
	pollUntil(t, p, func() bool { return !p.openingTerminal })
	if !called || p.directory != "src" || p.selected != 0 || p.preview.text != "package main" {
		t.Fatal("current-folder action lost its file selection or preview")
	}
}

func TestTerminalHereRejectsDirectoryReplacedBySymlinkBeforeOpen(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	selected := filepath.Join(root, "folder")
	if err := os.Mkdir(selected, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	pollUntil(t, p, func() bool { return p.selected == 0 })
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, selected); err != nil {
		t.Fatal(err)
	}
	p.SetTerminalHandler(func(*os.File, string) error { t.Fatal("followed a raced symlink"); return nil })
	p.terminalHere()
	pollUntil(t, p, func() bool { return !p.openingTerminal })
	if !strings.Contains(p.notice, "Unable to open terminal") || p.selected != 0 || p.directory != "" {
		t.Fatal("replaced folder did not report a safe launch error")
	}
}

func TestTerminalDirectoryReaderRejectsNonDirectoriesSymlinksAndTraversal(t *testing.T) {
	root := t.TempDir()
	putFile(t, filepath.Join(root, "regular"), []byte("text"))
	if err := os.Mkdir(filepath.Join(root, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("folder", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	reader := fixtureReader(t, root)
	for _, path := range []string{"regular", "link", "link/child", "../", "../outside", "/", "folder/../folder"} {
		res := reader.read(request{ctx: context.Background(), path: path, directory: true, openTerminal: true})
		if res.err == nil || res.file != nil {
			res.close()
			t.Fatalf("unsafe directory %q opened", path)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := reader.read(request{ctx: ctx, path: "folder", directory: true, openTerminal: true})
	if res.err == nil || res.file != nil {
		res.close()
		t.Fatal("canceled request returned an open directory")
	}
}
