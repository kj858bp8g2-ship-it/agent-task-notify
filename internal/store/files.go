package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

var errPrivate = errors.New("private state unavailable")

// Internal fixed labels support safe failure triage without retaining input or
// native errors. The public text remains identical for every failure.
type privateStateError struct{ stage, category string }

func (*privateStateError) Error() string { return "private state unavailable" }

// EnsurePrivateDirectory creates an owned directory with private access. Existing
// directories are checked, never repaired or recursively chmodded/chowned.
func EnsurePrivateDirectory(path string) error {
	if !safePath(path, true) {
		return errPrivate
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if nativeMkdir(path) != nil {
			return errPrivate
		}
	} else if err != nil || !info.IsDir() {
		return errPrivate
	}
	if !safePath(path, false) || !nativePrivate(path, true) {
		return errPrivate
	}
	return nil
}

// WriteAtomic writes a complete replacement in the same private directory.
// Sync is performed before replacement; this is not a power-loss guarantee.
func WriteAtomic(path string, data []byte) error {
	if !safeTarget(path) {
		return &privateStateError{"target-initial", "rejected"}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return &privateStateError{"random", nativeErrorCategory(err)}
	}
	temp := filepath.Join(filepath.Dir(path), ".tmp-"+hex.EncodeToString(random[:]))
	file, err := nativeOpen(temp, true)
	if err != nil {
		return &privateStateError{"create", nativeErrorCategory(err)}
	}
	// Only the exclusive temporary name owned by this invocation is removed.
	defer os.Remove(temp)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return &privateStateError{"write", nativeErrorCategory(err)}
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return &privateStateError{"sync", nativeErrorCategory(err)}
	}
	if err = file.Close(); err != nil {
		return &privateStateError{"close", nativeErrorCategory(err)}
	}
	if !safeTarget(path) {
		return &privateStateError{"target-recheck", "rejected"}
	}
	if err = nativeReplace(temp, path); err != nil {
		return &privateStateError{"replace", nativeErrorCategory(err)}
	}
	return nil
}

func safeTarget(path string) bool {
	if !safePath(path, true) || !nativePrivate(filepath.Dir(path), true) {
		return false
	}
	info, err := os.Lstat(path)
	return os.IsNotExist(err) || (err == nil && info.Mode().IsRegular() && nativePrivate(path, false))
}

// Reject links/reparse points in every existing component, not just the leaf.
// The caller owns the directory; this is not a sandbox against its own owner
// concurrently renaming parent directories.
func safePath(path string, missingLeaf bool) bool {
	return safePathFailure(path, missingLeaf) == nil
}

// The bool wrapper and parent diagnostics share this single traversal. Only
// fixed labels escape a rejected predicate; no diagnostic rescan is performed.
func safePathFailure(path string, missingLeaf bool) *privateStateError {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return &privateStateError{"path", "rejected"}
	}
	leaf := true
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			if !(leaf && missingLeaf && os.IsNotExist(err)) {
				if leaf {
					return &privateStateError{"leaf-stat", nativeErrorCategory(err)}
				}
				return &privateStateError{"ancestor-stat", nativeErrorCategory(err)}
			}
		} else {
			if info.Mode()&os.ModeSymlink != 0 {
				return &privateStateError{"symlink", "rejected"}
			}
			if failure := nativeReparseFailure(current); failure != nil {
				return failure
			}
			if !leaf {
				if !info.IsDir() {
					return &privateStateError{"ancestor-directory", "rejected"}
				}
				if failure := nativeTrustedAncestorFailure(current); failure != nil {
					return failure
				}
			}
		}
		if filepath.Dir(current) == current {
			break
		}
		leaf = false
	}
	return nil
}
