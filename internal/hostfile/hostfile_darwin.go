package hostfile

/*
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/acl.h>
#include <copyfile.h>
#include <fcntl.h>
#include <errno.h>
#include <stdlib.h>
#include <string.h>

static int host_encode_acl(acl_t acl, char **text, ssize_t *length) {
    *text = NULL;
    *length = 0;
    if (acl == NULL) return 0;
    if (acl_valid(acl) != 0) { acl_free(acl); return 0; }
    // Include the ACL-level flags even when the entry count is zero.
    ssize_t size = acl_size(acl);
    if (size <= 0) { acl_free(acl); return 0; }
    char *buffer = calloc(1, (size_t)size);
    if (buffer == NULL) { acl_free(acl); return 0; }
    ssize_t copied = acl_copy_ext(buffer, acl, size);
    acl_free(acl);
    if (copied != size) { free(buffer); return 0; }
    *text = buffer;
    *length = copied;
    return 1;
}

static int host_empty_acl(char **text, ssize_t *length) {
    return host_encode_acl(acl_init(0), text, length);
}

// An absent ACL is valid only after successful extended stat with all required
// properties populated. Never confuse a failed ACL lookup with no ACL.
static int host_acl(int fd, char **text, ssize_t *length) {
    *text = NULL;
    *length = 0;
    filesec_t security = filesec_init();
    if (security == NULL) return 0;
    struct stat st;
    int ok = fstatx_np(fd, &st, security) == 0 &&
        filesec_get_property(security, FILESEC_OWNER, NULL) == 0 &&
        filesec_get_property(security, FILESEC_GROUP, NULL) == 0 &&
        filesec_get_property(security, FILESEC_MODE, NULL) == 0;
    int present = 0;
    if (!ok || filesec_query_property(security, FILESEC_ACL, &present) != 0) {
        filesec_free(security); return 0;
    }
    if (!present) { filesec_free(security); return host_empty_acl(text, length); }
    acl_t acl = NULL;
    ok = filesec_get_property(security, FILESEC_ACL, &acl) == 0;
    filesec_free(security);
    if (!ok) return 0;
    // Portable external ACL representation retains GUIDs, ACE order and flags.
    return host_encode_acl(acl, text, length);
}

// Only called on an exclusively created empty temporary, never an Agent file.
static int host_clear_new_acl(int fd) {
    acl_t empty = acl_init(0);
    if (empty == NULL) return 0;
    int result = acl_set_fd_np(fd, empty, ACL_TYPE_EXTENDED);
    acl_free(empty);
    return result == 0;
}

static int host_copy_security(int from, int to) {
    return fcopyfile(from, to, NULL, COPYFILE_SECURITY);
}
*/
import "C"

import (
	"os"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

type nativeAccess struct {
	uid, gid uint32
	mode     uint16
	flags    uint32
	acl      string
}

func (access nativeAccess) canonical() []byte {
	return canonicalFields("darwin", strconv.FormatUint(uint64(access.uid), 10), strconv.FormatUint(uint64(access.gid), 10), strconv.FormatUint(uint64(access.mode), 10), strconv.FormatUint(uint64(access.flags), 10), access.acl)
}

func nativeDefaultAccess(parent string) (nativeAccess, error) {
	var st unix.Stat_t
	if unix.Lstat(parent, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || !ownedUID(st.Uid) {
		return nativeAccess{}, ErrUnsafe
	}
	// Darwin's BSD creation semantics inherit the parent directory's group.
	empty, err := emptyACL()
	if err != nil {
		return nativeAccess{}, ErrUnsafe
	}
	return nativeAccess{uid: uint32(os.Geteuid()), gid: st.Gid, mode: unix.S_IFREG | 0600, acl: empty}, nil
}

func emptyACL() (string, error) {
	var data *C.char
	var length C.ssize_t
	if C.host_empty_acl(&data, &length) != 1 {
		return "", ErrUnsafe
	}
	defer C.free(unsafe.Pointer(data))
	return string(C.GoBytes(unsafe.Pointer(data), C.int(length))), nil
}

func nativeExpectedAccess(access nativeAccess, existing bool) ([]nativeAccess, error) {
	if !access.writable() {
		return nil, ErrUnsafe
	}
	return []nativeAccess{access}, nil
}

func preservedAccess(source, destination nativeAccess) bool { return source == destination }

func (access nativeAccess) writable() bool {
	return access.mode&0200 != 0 && access.flags&(unix.UF_IMMUTABLE|unix.UF_APPEND|unix.SF_IMMUTABLE|unix.SF_APPEND|unix.SF_RESTRICTED) == 0
}

func nativePath(path string) bool { return true }
func nativeUnlinked(path string) bool {
	var st unix.Stat_t
	return unix.Lstat(path, &st) == nil && st.Mode&unix.S_IFMT != unix.S_IFLNK
}

func ownedUID(uid uint32) bool { return uid == uint32(os.Geteuid()) }

func nativeParent(path string) (os.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnsafe
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || !ownedUID(st.Uid) || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, ErrUnsafe
	}
	return file.Stat()
}

func nativeOpen(path string, writable bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	if writable {
		flags = unix.O_RDWR | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func nativeMetadata(file *os.File) (nativeAccess, error) {
	fd := int(file.Fd())
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || !ownedUID(st.Uid) || st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nativeAccess{}, ErrUnsafe
	}
	var data *C.char
	var length C.ssize_t
	if C.host_acl(C.int(fd), &data, &length) != 1 {
		return nativeAccess{}, ErrUnsafe
	}
	defer C.free(unsafe.Pointer(data))
	if length < 0 || uint64(length) > 1<<20 {
		return nativeAccess{}, ErrUnsafe
	}
	return nativeAccess{st.Uid, st.Gid, st.Mode, st.Flags, string(C.GoBytes(unsafe.Pointer(data), C.int(length)))}, nil
}

func nativeCreate(path string, newTarget bool) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	identity, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, ErrUnsafe
	}
	// Removing inherited permissions is permitted only for our empty new file.
	if C.host_clear_new_acl(C.int(fd)) != 1 || unix.Fchmod(fd, 0600) != nil {
		file.Close()
		cleanupTemporary(path, identity)
		return nil, ErrUnsafe
	}
	access, err := nativeMetadata(file)
	empty, emptyErr := emptyACL()
	if err != nil || emptyErr != nil || access.mode != unix.S_IFREG|0600 || access.acl != empty || access.flags != 0 {
		file.Close()
		cleanupTemporary(path, identity)
		return nil, ErrUnsafe
	}
	return file, nil
}

func nativeCopySecurity(source, destination *os.File, access nativeAccess) error {
	if !access.writable() || C.host_copy_security(C.int(source.Fd()), C.int(destination.Fd())) != 0 {
		return ErrUnsafe
	}
	return nil // The common caller verifies every copied field before writing.
}

func nativeReplace(from, to string, identity os.FileInfo, access nativeAccess) error {
	file, err := nativeOpen(from, false)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(identity, info) {
		return ErrUnsafe
	}
	actual, err := nativeMetadata(file)
	if err != nil || actual != access {
		return ErrUnsafe
	}
	return unix.Rename(from, to)
}
