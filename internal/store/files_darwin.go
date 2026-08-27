package store

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func nativeErrorCategory(err error) string {
	switch {
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
		return "access"
	case errors.Is(err, unix.ENOENT):
		return "missing"
	case errors.Is(err, unix.EEXIST):
		return "exists"
	case errors.Is(err, unix.EIO), errors.Is(err, unix.ENOSPC):
		return "io"
	default:
		return "other"
	}
}

func nativeMkdir(path string) error {
	if err := os.Mkdir(path, 0700); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errPrivate
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&unix.S_IFMT != unix.S_IFDIR || !clearNewFileACL(fd) || unix.Fchmod(fd, 0700) != nil {
		return errPrivate
	}
	return nil
}
func nativeReparse(path string) bool { return false } // lstat handles Darwin symlinks.
func nativePrivate(path string, directory bool) bool {
	var stat unix.Stat_t
	if unix.Lstat(path, &stat) != nil || stat.Uid != uint32(os.Geteuid()) {
		return false
	}
	want := uint16(unix.S_IFREG | 0600)
	if directory {
		want = unix.S_IFDIR | 0700
	}
	return stat.Mode == want && emptyPathACL(path)
}
func nativeOpen(path string, exclusive bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_NOFOLLOW | unix.O_CLOEXEC
	// Distinguish a newly created lock file from an existing one; only newly
	// created objects may have inherited ACLs removed.
	fd, err := unix.Open(path, flags|unix.O_CREAT|unix.O_EXCL, 0600)
	created := err == nil
	if err == unix.EEXIST && !exclusive {
		fd, err = unix.Open(path, flags, 0)
	}
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return nil, errPrivate
	}
	if created {
		if !clearNewFileACL(fd) || unix.Fchmod(fd, 0600) != nil {
			unix.Close(fd)
			os.Remove(path)
			return nil, errPrivate
		}
	}
	if unix.Fstat(fd, &stat) != nil || stat.Mode != unix.S_IFREG|0600 || !emptyFileACL(fd) {
		unix.Close(fd)
		if created {
			os.Remove(path)
		}
		return nil, errPrivate
	}
	return os.NewFile(uintptr(fd), path), nil
}
func nativeReplace(from, to string) error { return os.Rename(from, to) }
