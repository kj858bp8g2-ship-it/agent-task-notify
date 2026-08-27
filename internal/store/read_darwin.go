package store

import (
	"os"

	"golang.org/x/sys/unix"
)

func nativeReadOpen(path string) (*os.File, error) {
	// NONBLOCK also prevents a raced FIFO replacement from hanging at open.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errPrivate
	}
	file := os.NewFile(uintptr(fd), path)
	if !nativeHandlePrivate(file, false) {
		file.Close()
		return nil, errPrivate
	}
	return file, nil
}

func nativeHandlePrivate(file *os.File, directory bool) bool {
	fd := int(file.Fd())
	var stat unix.Stat_t
	want := uint16(unix.S_IFREG | 0600)
	if directory {
		want = unix.S_IFDIR | 0700
	}
	return unix.Fstat(fd, &stat) == nil && stat.Uid == uint32(os.Geteuid()) && stat.Mode == want && emptyFileACL(fd)
}
