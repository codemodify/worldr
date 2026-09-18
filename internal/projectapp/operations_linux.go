//go:build linux

package projectapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func (r *directoryReader) parentFD(ctx context.Context, path string) (*os.File, string, error) {
	if path == "" || !cleanRelative(path) {
		return nil, "", fmt.Errorf("file path must stay inside the project")
	}
	directory := filepath.Dir(path)
	if directory == "." {
		directory = ""
	}
	file, err := r.open(ctx, directory, true)
	return file, filepath.Base(path), err
}

func statEntry(parent *os.File, name string) (fileStamp, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fileStamp{}, err
	}
	return stampOf(&stat), nil
}

func sameEntry(a, b fileStamp) bool {
	return a.device == b.device && a.inode == b.inode && a.mode&unix.S_IFMT == b.mode&unix.S_IFMT
}

func uniqueOperationName() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(random[:]), nil
}

func (r *directoryReader) performOperation(req request) result {
	res := result{request: req}
	op := *req.operation
	if err := req.ctx.Err(); err != nil {
		res.err = err
		return res
	}
	if op.action == undoFileAction {
		if op.undo == nil {
			res.err = fmt.Errorf("no file operation to undo")
			return res
		}
		if op.undo.created {
			res.err = r.undoCreation(req.ctx, *op.undo)
			res.notice = "Undid file creation; the created item is recoverable in project Trash."
			return res
		}
		op.source, op.target, op.expected = op.undo.target, op.undo.source, op.undo.expected
	}
	if op.source == projectTrash || op.target == projectTrash {
		res.err = fmt.Errorf("the project Trash folder is reserved; move its contents to restore them")
		return res
	}
	if op.action == createFolder {
		res.undo, res.err = r.createDirectory(req.ctx, op.target)
	} else if op.action == duplicateItem {
		res.undo, res.err = r.duplicateFile(req.ctx, op)
	} else {
		if op.action == restoreTrashItem {
			op.target, res.err = r.trashOriginal(req.ctx, op.source)
			if res.err != nil {
				return res
			}
		}
		if op.action == trashItem {
			if strings.HasPrefix(op.source, projectTrash+"/") {
				res.err = fmt.Errorf("already in project Trash; use Move to restore this item")
				return res
			}
			var bucket string
			bucket, res.err = r.makeTrashBucket(req.ctx, op.source)
			if res.err != nil {
				return res
			}
			op.target = filepath.Join(bucket, "items", filepath.Base(op.source))
			defer func() {
				if res.err != nil {
					parent, name, err := r.parentFD(context.Background(), bucket)
					if err == nil {
						defer parent.Close()
						_ = unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
					}
				}
			}()
		}
		res.undo, res.err = r.renameEntry(req.ctx, op)
	}
	if res.err == nil {
		res.notice = operationNotice(req.operation.action, op.target)
		parent := filepath.Dir(op.target)
		if parent == "." {
			parent = ""
		}
		if parent == req.path {
			res.selected = filepath.Base(op.target)
		}
	}
	return res
}

func (r *directoryReader) createDirectory(ctx context.Context, path string) (*fileUndo, error) {
	parent, name, err := r.parentFD(ctx, path)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if !validNewName(name) {
		return nil, fmt.Errorf("invalid folder name")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	random, err := uniqueOperationName()
	if err != nil {
		return nil, err
	}
	temporary := ".worldr-folder-" + random
	if err = unix.Mkdirat(int(parent.Fd()), temporary, 0755); err != nil {
		return nil, err
	}
	defer func() { _ = unix.Unlinkat(int(parent.Fd()), temporary, unix.AT_REMOVEDIR) }()
	fd, err := unix.Openat(int(parent.Fd()), temporary, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	created := os.NewFile(uintptr(fd), temporary)
	defer created.Close()
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = unix.Renameat2(int(parent.Fd()), temporary, int(parent.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		return nil, err
	}
	return &fileUndo{target: path, expected: stampOf(&stat), created: true, directory: true}, nil
}

func (r *directoryReader) renameEntry(ctx context.Context, op fileOperation) (*fileUndo, error) {
	if op.source == op.target {
		return nil, fmt.Errorf("source and destination are the same")
	}
	source, name, err := r.parentFD(ctx, op.source)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	before, err := statEntry(source, name)
	if err != nil {
		return nil, err
	}
	if before.mode&unix.S_IFMT != unix.S_IFREG && before.mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, fmt.Errorf("symbolic links and special files cannot be moved")
	}
	if !sameEntry(before, op.expected) {
		return nil, fmt.Errorf("the selected item changed; refresh and try again")
	}
	target, newName, err := r.parentFD(ctx, op.target)
	if err != nil {
		return nil, err
	}
	defer target.Close()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = unix.Renameat2(int(source.Fd()), name, int(target.Fd()), newName, unix.RENAME_NOREPLACE); err != nil {
		return nil, err
	}
	after, err := statEntry(target, newName)
	if err != nil || !sameEntry(before, after) {
		rollback := unix.Renameat2(int(target.Fd()), newName, int(source.Fd()), name, unix.RENAME_NOREPLACE)
		if rollback != nil {
			return nil, fmt.Errorf("item changed during move; inspect %s and %s before retrying", safeLabel(op.source), safeLabel(op.target))
		}
		return nil, fmt.Errorf("item changed during move; original path restored")
	}
	return &fileUndo{source: op.source, target: op.target, expected: after}, nil
}

type trashRecord struct {
	Version  int    `json:"version"`
	Original []byte `json:"original"`
	Display  string `json:"display"`
}

func (r *directoryReader) makeTrashBucket(ctx context.Context, original string) (string, error) {
	if err := unix.Mkdirat(int(r.root.Fd()), projectTrash, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return "", err
	}
	trash, err := r.open(ctx, projectTrash, true)
	if err != nil {
		return "", err
	}
	defer trash.Close()
	var stat unix.Stat_t
	if err = unix.Fstat(int(trash.Fd()), &stat); err != nil {
		return "", err
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 {
		return "", fmt.Errorf("project Trash must be a private folder owned by this user")
	}
	name, err := uniqueOperationName()
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = unix.Mkdirat(int(trash.Fd()), name, 0700); err != nil {
		return "", err
	}
	bucketPath := filepath.Join(projectTrash, name)
	bucket, err := r.open(ctx, bucketPath, true)
	if err != nil {
		return "", err
	}
	defer bucket.Close()
	if err = unix.Mkdirat(int(bucket.Fd()), "items", 0700); err != nil {
		return "", err
	}
	fd, err := unix.Openat(int(bucket.Fd()), "restore.json", unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return "", err
	}
	metadata := os.NewFile(uintptr(fd), "restore.json")
	defer metadata.Close()
	// []byte retains arbitrary Linux filename bytes without lossy JSON UTF-8
	// replacement; Display is informational only and never used for traversal.
	if err = json.NewEncoder(metadata).Encode(trashRecord{Version: 1, Original: []byte(original), Display: safeLabel(original)}); err != nil {
		return "", err
	}
	if err = metadata.Sync(); err != nil {
		return "", err
	}
	return bucketPath, nil
}

func (r *directoryReader) trashOriginal(ctx context.Context, source string) (string, error) {
	if !restorableTrashItem(source) {
		return "", fmt.Errorf("select the trashed file or folder inside its items directory")
	}
	bucket := filepath.Dir(filepath.Dir(source))
	metadata, err := r.open(ctx, filepath.Join(bucket, "restore.json"), false)
	if err != nil {
		return "", fmt.Errorf("Trash recovery metadata is unavailable; use Move to restore manually: %w", err)
	}
	defer metadata.Close()
	data, err := io.ReadAll(io.LimitReader(metadata, 16<<10+1))
	if err != nil {
		return "", err
	}
	if len(data) > 16<<10 {
		return "", fmt.Errorf("Trash recovery metadata exceeds its limit")
	}
	var record trashRecord
	if err = json.Unmarshal(data, &record); err != nil {
		return "", fmt.Errorf("invalid Trash recovery metadata: %w", err)
	}
	original := string(record.Original)
	if record.Version != 1 || original == "" || !cleanRelative(original) || filepath.Base(original) != filepath.Base(source) || original == projectTrash || strings.HasPrefix(original, projectTrash+"/") {
		return "", fmt.Errorf("invalid original Trash location; use Move to restore manually")
	}
	return original, nil
}

func (r *directoryReader) duplicateFile(ctx context.Context, op fileOperation) (*fileUndo, error) {
	source, err := r.open(ctx, op.source, false)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	var before unix.Stat_t
	if err = unix.Fstat(int(source.Fd()), &before); err != nil {
		return nil, err
	}
	stamp := stampOf(&before)
	if !sameEntry(stamp, op.expected) {
		return nil, fmt.Errorf("the selected file changed; refresh and try again")
	}
	if stamp.size > maxDuplicateBytes {
		return nil, fmt.Errorf("Duplicate is limited to 128 MiB per file")
	}
	parent, name, err := r.parentFD(ctx, op.target)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if !validNewName(name) {
		return nil, fmt.Errorf("invalid duplicate filename")
	}
	random, err := uniqueOperationName()
	if err != nil {
		return nil, err
	}
	temporary := ".worldr-copy-" + random
	fd, err := unix.Openat(int(parent.Fd()), temporary, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	copyFile := os.NewFile(uintptr(fd), temporary)
	defer copyFile.Close()
	defer func() { _ = unix.Unlinkat(int(parent.Fd()), temporary, 0) }()
	data := make([]byte, 32<<10)
	var total int64
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := source.Read(data)
		total += int64(n)
		if total > maxDuplicateBytes {
			return nil, fmt.Errorf("Duplicate is limited to 128 MiB per file")
		}
		if n > 0 {
			if _, err = copyFile.Write(data[:n]); err != nil {
				return nil, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	var after unix.Stat_t
	if err = unix.Fstat(int(source.Fd()), &after); err != nil {
		return nil, err
	}
	if stampOf(&after) != stamp {
		return nil, fmt.Errorf("source changed while copying; no duplicate was installed")
	}
	if err = copyFile.Chmod(os.FileMode(before.Mode & 0777)); err != nil {
		return nil, err
	}
	if err = copyFile.Sync(); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = unix.Renameat2(int(parent.Fd()), temporary, int(parent.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		return nil, err
	}
	var created unix.Stat_t
	if err = unix.Fstat(int(copyFile.Fd()), &created); err != nil {
		return nil, fmt.Errorf("duplicate created, but identity check failed: %w", err)
	}
	return &fileUndo{target: op.target, expected: stampOf(&created), created: true}, nil
}

func (r *directoryReader) undoCreation(ctx context.Context, undo fileUndo) error {
	parent, name, err := r.parentFD(ctx, undo.target)
	if err != nil {
		return err
	}
	defer parent.Close()
	stamp, err := statEntry(parent, name)
	if err != nil {
		return err
	}
	if stamp != undo.expected {
		return fmt.Errorf("created item has changed; Undo will not remove it")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	bucket, err := r.makeTrashBucket(ctx, undo.target)
	if err != nil {
		return err
	}
	_, err = r.renameEntry(ctx, fileOperation{source: undo.target, target: filepath.Join(bucket, "items", name), expected: stamp})
	return err
}
