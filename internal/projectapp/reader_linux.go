//go:build linux

package projectapp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/resourcepath"
	"golang.org/x/sys/unix"
)

type directoryReader struct{ root *os.File }

func (*directoryReader) supportsThumbnails() bool { return true }

func openReader(root string) (projectReader, string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, "", err
	}
	// Resolving the explicitly supplied startup root is permitted. Navigation
	// below this fixed anchor never follows a symlink, even after replacement.
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, "", fmt.Errorf("project root: %w", err)
	}
	fd, err := unix.Open(absolute, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, "", fmt.Errorf("project root: %w", err)
	}
	return &directoryReader{root: os.NewFile(uintptr(fd), absolute)}, absolute, nil
}

// Saved roots already contain their canonical path. Reopen it exactly once
// without resolving a replacement final symlink; explicit New roots retain the
// separate user-selected symlink behavior in openReader.
func openSessionReader(root string) (projectReader, string, error) {
	file, err := resourcepath.OpenDirectory(root)
	if err != nil {
		return nil, "", fmt.Errorf("project root: %w", err)
	}
	return &directoryReader{root: file}, root, nil
}

func (r *directoryReader) close() error { return r.root.Close() }

// sessionRoot runs only on the owning host goroutine while the provider is
// open. The worker does not close its root until Close cancels it. Verify any
// pathname against the anchor so a rename/replacement never saves a different
// project, and never serialize Linux's synthetic " (deleted)" FD link target.
func (r *directoryReader) sessionRoot() string {
	path, _ := resourcepath.FromFile(r.root)
	return path
}

func (r *directoryReader) open(ctx context.Context, path string, directory bool) (*os.File, error) {
	if filepath.IsAbs(path) || path != "" && filepath.Clean(path) != path || path == ".." || strings.HasPrefix(path, "../") {
		return nil, fmt.Errorf("path is outside the project root")
	}
	parts := strings.Split(path, string(filepath.Separator))
	if path == "" {
		parts = []string{"."}
	}
	parent := r.root
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			if parent != r.root {
				_ = parent.Close()
			}
			return nil, err
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if directory || i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		} else {
			var stat unix.Stat_t
			err := unix.Fstatat(int(parent.Fd()), part, &stat, unix.AT_SYMLINK_NOFOLLOW)
			if err == nil && stat.Mode&unix.S_IFMT != unix.S_IFREG {
				err = fmt.Errorf("only regular files can be opened")
			}
			if err != nil {
				if parent != r.root {
					_ = parent.Close()
				}
				return nil, err
			}
		}
		fd, err := unix.Openat(int(parent.Fd()), part, flags, 0)
		if parent != r.root {
			_ = parent.Close()
		}
		if err != nil {
			return nil, err
		}
		parent = os.NewFile(uintptr(fd), part)
	}
	return parent, nil
}

func (r *directoryReader) read(req request) result {
	if req.operation != nil {
		return r.performOperation(req)
	}
	if req.thumbnail {
		return r.readThumbnail(req)
	}
	res := result{request: req}
	file, err := r.open(req.ctx, req.path, req.directory || req.openTerminal)
	if err != nil {
		res.err = err
		return res
	}
	if req.openMedia || req.openTerminal {
		// Check the open descriptor itself before transferring ownership. A
		// later browser cancellation must not close a successfully launched file.
		info, err := file.Stat()
		if err == nil {
			if req.openTerminal && !info.IsDir() {
				err = fmt.Errorf("only directories can be opened for Terminal Here")
			} else if !req.openTerminal && !info.Mode().IsRegular() {
				err = fmt.Errorf("only regular files can be opened")
			}
		}
		if err == nil {
			err = req.ctx.Err()
		}
		if err != nil {
			_ = file.Close()
			res.err = err
		} else {
			res.file = file
		}
		return res
	}
	defer file.Close()
	stop := context.AfterFunc(req.ctx, func() { _ = file.Close() })
	defer stop()
	if req.directory {
		entries, err := file.ReadDir(maxEntries + 1)
		if err != nil && err != io.EOF {
			res.err = err
			return res
		}
		res.truncated = len(entries) > maxEntries
		if res.truncated {
			entries = entries[:maxEntries]
		}
		res.entries = make([]entry, 0, len(entries))
		for _, item := range entries {
			if err := req.ctx.Err(); err != nil {
				res.err = err
				return res
			}
			kind := specialEntry
			var stat unix.Stat_t
			if withFileDescriptor(file, func(fd int) error { return unix.Fstatat(fd, item.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW) }) != nil {
				continue // The item disappeared while this bounded listing was read.
			}
			switch stat.Mode & unix.S_IFMT {
			case unix.S_IFLNK:
				kind = symlinkEntry
			case unix.S_IFDIR:
				kind = directoryEntry
			case unix.S_IFREG:
				kind = fileEntry
			}
			res.entries = append(res.entries, entry{name: item.Name(), kind: kind, stamp: stampOf(&stat)})
		}
		sort.Slice(res.entries, func(i, j int) bool {
			a, b := res.entries[i], res.entries[j]
			if (a.kind == directoryEntry) != (b.kind == directoryEntry) {
				return a.kind == directoryEntry
			}
			al, bl := strings.ToLower(a.name), strings.ToLower(b.name)
			if al == bl {
				return a.name < b.name
			}
			return al < bl
		})
		return res
	}
	info, err := file.Stat()
	if err != nil {
		res.err = err
		return res
	}
	if !info.Mode().IsRegular() {
		res.err = fmt.Errorf("only regular files have a text preview")
		return res
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPreview+1))
	if err != nil {
		res.err = err
		return res
	}
	res.truncated = len(data) > maxPreview
	if res.truncated {
		data = data[:maxPreview]
		// A limit may split one final UTF-8 character; trim only an incomplete
		// suffix. Invalid bytes elsewhere still reject the binary preview.
		start := len(data) - 1
		for start > 0 && !utf8.RuneStart(data[start]) && len(data)-start < utf8.UTFMax {
			start--
		}
		if !utf8.FullRune(data[start:]) {
			data = data[:start]
		}
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		res.err = fmt.Errorf("binary or non-UTF-8 file; text preview unavailable")
		return res
	}
	res.preview = makePreview(string(data))
	return res
}

func stampOf(stat *unix.Stat_t) fileStamp {
	return fileStamp{device: uint64(stat.Dev), inode: stat.Ino, size: stat.Size, modified: stat.Mtim.Nano(), mode: stat.Mode}
}

// Control pins the descriptor across a syscall even when cancellation closes
// the os.File. Borrowing Fd directly would race Close and descriptor reuse.
func withFileDescriptor(file *os.File, action func(int) error) error {
	connection, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var operationErr error
	if err := connection.Control(func(fd uintptr) { operationErr = action(int(fd)) }); err != nil {
		return err
	}
	return operationErr
}
