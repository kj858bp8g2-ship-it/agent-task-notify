// Package hostfile reads and changes owned Agent configuration files.
package hostfile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
)

var (
	ErrUnsafe       = errors.New("host file unsafe or unavailable")
	ErrConflict     = errors.New("host file changed")
	ErrVerification = errors.New("host file committed but verification failed")
)

// Snapshot is bound to one absolute, clean path. Data and Exists are views for
// callers; modifying them cannot change the private comparison authority.
type Snapshot struct {
	Data   []byte
	Exists bool
	state  *observation
}

// AccessDigest exposes only a versioned, domain-separated SHA-256 access-state
// digest. It does not expose permissions, principals, paths or file contents.
func (s Snapshot) AccessDigest() (string, error) {
	if s.state == nil {
		return "", ErrUnsafe
	}
	return accessDigest(s.state.exists, s.state.access), nil
}

// ExpectedAccessDigests predicts supported post-operation access, not authority
// to mutate a file. A receipt must also compare bytes and existence. Predictions
// use only captured metadata and never read or write the current filesystem.
func (s Snapshot) ExpectedAccessDigests(exists bool) ([]string, error) {
	if s.state == nil {
		return nil, ErrUnsafe
	}
	if !exists {
		return []string{accessDigest(false, nativeAccess{})}, nil
	}
	access := s.state.access
	if !s.state.exists {
		access = s.state.newAccess
	}
	states, err := nativeExpectedAccess(access, s.state.exists)
	if err != nil {
		return nil, ErrUnsafe
	}
	var result []string
	for _, value := range states {
		digest := accessDigest(true, value)
		if len(result) == 0 || result[0] != digest {
			result = append(result, digest)
		}
	}
	if len(result) == 0 || len(result) > 2 {
		return nil, ErrUnsafe
	}
	return result, nil
}

func accessDigest(exists bool, access nativeAccess) string {
	value := []byte("agent-task-notify/hostfile/access/v1\x00")
	if exists {
		value = append(value, 1)
		value = append(value, access.canonical()...)
	} else {
		value = append(value, 0)
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func canonicalFields(values ...string) []byte {
	var result []byte
	for _, value := range values {
		result = binary.BigEndian.AppendUint64(result, uint64(len(value)))
		result = append(result, value...)
	}
	return result
}

type observation struct {
	path         string
	limit        int64
	exists       bool
	parent, file os.FileInfo
	access       nativeAccess
	newAccess    nativeAccess
	digest       [sha256.Size]byte
}

// Read does not create files, change access rights or follow links. The direct
// parent must be owned, but need not be private. Every ancestor is link-free.
func Read(path string, maxBytes int64) (Snapshot, error) {
	snapshot, file, err := observe(path, maxBytes, false)
	if file != nil {
		file.Close()
	}
	return snapshot, err
}

func parentInfo(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !nativePath(path) {
		return nil, ErrUnsafe
	}
	parent := filepath.Dir(path)
	if parent == path {
		return nil, ErrUnsafe
	}
	for current := parent; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !nativeUnlinked(current) {
			return nil, ErrUnsafe
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nativeParent(parent)
}

func sameVersion(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}

func observe(path string, limit int64, writable bool) (Snapshot, *os.File, error) {
	if limit <= 0 || limit == math.MaxInt64 {
		return Snapshot{}, nil, ErrUnsafe
	}
	parent, err := parentInfo(path)
	if err != nil {
		return Snapshot{}, nil, ErrUnsafe
	}
	state := &observation{path: path, limit: limit, parent: parent}
	file, err := nativeOpen(path, writable)
	if err != nil {
		if os.IsNotExist(err) {
			// Distinguish a genuinely missing leaf from missing path components.
			current, parentErr := parentInfo(path)
			_, leafErr := os.Lstat(path)
			if parentErr == nil && os.SameFile(parent, current) && os.IsNotExist(leafErr) {
				state.newAccess, err = nativeDefaultAccess(filepath.Dir(path))
				if err != nil {
					return Snapshot{}, nil, ErrUnsafe
				}
				return Snapshot{state: state}, nil, nil
			}
		}
		return Snapshot{}, nil, ErrUnsafe
	}
	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()
	access, err := nativeMetadata(file)
	if err != nil || (writable && !access.writable()) {
		return Snapshot{}, nil, ErrUnsafe
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return Snapshot{}, nil, ErrUnsafe
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		clear(data)
		return Snapshot{}, nil, ErrUnsafe
	}
	after, err := file.Stat()
	accessAfter, accessErr := nativeMetadata(file)
	current, pathErr := os.Lstat(path)
	parentAfter, parentErr := parentInfo(path)
	if err != nil || accessErr != nil || access != accessAfter || !sameVersion(info, after) || pathErr != nil || !current.Mode().IsRegular() || !os.SameFile(after, current) || parentErr != nil || !os.SameFile(parent, parentAfter) || !nativeUnlinked(path) {
		clear(data)
		return Snapshot{}, nil, ErrUnsafe
	}
	state.exists, state.file, state.access, state.digest = true, after, access, sha256.Sum256(data)
	ok = true
	return Snapshot{Data: data, Exists: true, state: state}, file, nil
}

func sameObservation(before, live *observation) bool {
	if before == nil || live == nil || before.path != live.path || before.exists != live.exists || !os.SameFile(before.parent, live.parent) {
		return false
	}
	return (!before.exists && before.newAccess == live.newAccess) || (before.exists && sameVersion(before.file, live.file) && before.access == live.access && before.digest == live.digest)
}

func matching(path string, before Snapshot, writable bool) (Snapshot, *os.File, error) {
	if before.state == nil || before.state.path != path {
		return Snapshot{}, nil, ErrConflict
	}
	live, file, err := observe(path, before.state.limit, writable)
	if err != nil {
		return Snapshot{}, nil, err
	}
	if !sameObservation(before.state, live.state) {
		if file != nil {
			file.Close()
		}
		return Snapshot{}, nil, ErrConflict
	}
	return live, file, nil
}

// prepareTemporary returns an empty file whose access is already final. No
// replacement bytes are written until metadata copy and handle verification
// have both succeeded. Only this exclusively created object may be cleaned up.
func prepareTemporary(path string, source *os.File, access nativeAccess) (*os.File, nativeAccess, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, nativeAccess{}, ErrUnsafe
	}
	temp := filepath.Join(filepath.Dir(path), ".host-tmp-"+hex.EncodeToString(random[:]))
	file, err := nativeCreate(temp)
	if err != nil {
		return nil, nativeAccess{}, ErrUnsafe
	}
	identity, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nativeAccess{}, ErrUnsafe
	}
	if source != nil {
		current, readErr := nativeMetadata(source)
		if readErr != nil || current != access || nativeCopySecurity(source, file, access) != nil {
			file.Close()
			cleanupTemporary(temp, identity)
			return nil, nativeAccess{}, ErrUnsafe
		}
	}
	actual, err := nativeMetadata(file)
	if err != nil || (source != nil && !preservedAccess(access, actual)) {
		file.Close()
		cleanupTemporary(temp, identity)
		return nil, nativeAccess{}, ErrUnsafe
	}
	return file, actual, nil
}

func cleanupTemporary(path string, identity os.FileInfo) {
	current, err := os.Lstat(path)
	if err == nil && current.Mode().IsRegular() && os.SameFile(identity, current) && nativeUnlinked(path) {
		_ = os.Remove(path)
	}
}

// Replace detects observed edits, not adversarial compare-and-swap. Third
// parties do not honor our checks: a final-check/name-replacement race remains,
// as does same-owner ancestor renaming. Sync is not a power-loss guarantee.
// ErrVerification means the name change committed; do not claim rollback or
// discard the installer's pending receipt when it is returned.
func Replace(path string, before Snapshot, replacement []byte) error {
	live, source, err := matching(path, before, true)
	if err != nil {
		return err
	}
	if source != nil {
		defer source.Close()
	}
	temp, expected, err := prepareTemporary(path, source, live.state.access)
	if err != nil {
		return err
	}
	identity, err := temp.Stat()
	if err != nil {
		temp.Close()
		return ErrUnsafe
	}
	defer cleanupTemporary(temp.Name(), identity)
	if source == nil && expected != live.state.newAccess {
		temp.Close()
		return ErrUnsafe
	}
	if _, err := temp.Write(replacement); err != nil {
		temp.Close()
		return ErrUnsafe
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return ErrUnsafe
	}
	if err := temp.Close(); err != nil {
		return ErrUnsafe
	}
	_, checked, err := matching(path, before, true)
	if checked != nil {
		checked.Close()
	}
	if err != nil {
		return err
	}
	if err := nativeReplace(temp.Name(), path, identity, expected); err != nil {
		return ErrUnsafe
	}
	return verifyReplacement(path, replacement, expected, identity)
}

func verifyReplacement(path string, replacement []byte, access nativeAccess, identity os.FileInfo) error {
	limit := int64(len(replacement))
	if limit == 0 {
		limit = 1
	}
	after, err := Read(path, limit)
	if err != nil || !after.Exists || !os.SameFile(identity, after.state.file) || after.state.digest != sha256.Sum256(replacement) || after.state.access != access {
		return ErrVerification
	}
	return nil
}

// Remove deletes only the one matching regular file, never recursively. The
// same final-check/name-operation race described for Replace applies here.
func Remove(path string, before Snapshot) error {
	if before.state == nil || !before.state.exists {
		return ErrConflict
	}
	_, file, err := matching(path, before, true)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := os.Remove(path); err != nil {
		return ErrUnsafe
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return ErrVerification
	}
	return nil
}
