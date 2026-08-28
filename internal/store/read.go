package store

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
)

var ErrNotFound = errors.New("private state not found")

// CheckPrivateDirectoryParent validates a proposed, still-missing data root.
// Its direct parent must exist; all ancestors must be link-free and owned by
// the current user or trusted system principals. It never creates or repairs.
func CheckPrivateDirectoryParent(path string) error {
	_, _, err := CheckPrivateDirectoryParentDiagnostic(path)
	return err
}

// CheckPrivateDirectoryParentDiagnostic performs the same read-only check and
// returns its first rejection's fixed labels, never paths or native errors.
// Success has empty labels; rejection preserves the original errPrivate.
func CheckPrivateDirectoryParentDiagnostic(path string) (stage, category string, err error) {
	if failure := safePathFailure(path, true); failure != nil {
		return failure.stage, failure.category, errPrivate
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		category := "exists"
		if err != nil {
			category = nativeErrorCategory(err)
		}
		return "leaf-missing", category, errPrivate
	}
	return "", "", nil
}

// CheckPrivateDirectory only validates; it never creates or repairs metadata.
func CheckPrivateDirectory(path string) error {
	if !safePath(path, false) || !nativePrivate(path, true) {
		return errPrivate
	}
	return nil
}

func privateReadTarget(path string) error {
	if !safePath(path, true) || CheckPrivateDirectory(filepath.Dir(path)) != nil {
		return errPrivate
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	if err != nil || !info.Mode().IsRegular() || !nativePrivate(path, false) {
		return errPrivate
	}
	return nil
}

// ReadPrivate opens an existing regular private file without following links.
// A rename by the same directory owner during the read fails closed.
func ReadPrivate(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes == math.MaxInt64 {
		return nil, errPrivate
	}
	if err := privateReadTarget(path); err != nil {
		return nil, err
	}
	file, err := nativeReadOpen(path)
	if err != nil {
		return nil, errPrivate
	}
	defer file.Close()
	if !samePrivateFile(file, path) {
		return nil, errPrivate
	}
	info, err := file.Stat()
	if err != nil || info.Size() > maxBytes {
		return nil, errPrivate
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes || !samePrivateFile(file, path) {
		clear(data)
		return nil, errPrivate
	}
	return data, nil
}

func samePrivateFile(file *os.File, path string) bool {
	if privateReadTarget(path) != nil || !nativeHandlePrivate(file, false) {
		return false
	}
	opened, err := file.Stat()
	if err != nil {
		return false
	}
	current, err := os.Lstat(path)
	return err == nil && os.SameFile(opened, current)
}

// RemovePrivate removes exactly one checked regular file, never a directory.
// As with WriteAtomic, this is not a sandbox against the directory's owner
// concurrently changing path components.
func RemovePrivate(path string) error {
	if err := privateReadTarget(path); err != nil {
		return err
	}
	file, err := nativeReadOpen(path)
	if err != nil {
		return errPrivate
	}
	defer file.Close()
	if !samePrivateFile(file, path) || os.Remove(path) != nil {
		return errPrivate
	}
	return nil
}
