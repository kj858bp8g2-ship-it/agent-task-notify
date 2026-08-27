package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDarwinPrivateReadRejectsModesLinksAndFIFO(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "state")
	if err := WriteAtomic(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(dir, "ancestor")
	if err := os.Symlink(dir, ancestor); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateDirectoryParent(filepath.Join(ancestor, "missing")); err != errPrivate {
		t.Fatal("linked parent accepted")
	}
	// Exercise open itself, not just ReadPrivate's pre-open lstat gate. This
	// catches blocking when an owner swaps in a FIFO between the two checks.
	opened := make(chan error, 1)
	go func() {
		file, err := nativeReadOpen(fifo)
		if file != nil {
			file.Close()
		}
		opened <- err
	}()
	select {
	case err := <-opened:
		if err != errPrivate {
			t.Fatal("FIFO handle accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO open blocked")
	}
	for _, candidate := range []string{link, filepath.Join(ancestor, "state"), fifo} {
		start := time.Now()
		if _, err := ReadPrivate(candidate, 1024); err != errPrivate {
			t.Fatal("unsafe read accepted")
		}
		if err := RemovePrivate(candidate); err != errPrivate {
			t.Fatal("unsafe removal accepted")
		}
		if time.Since(start) > time.Second {
			t.Fatal("nonregular open blocked")
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivate(path, 1024); err != errPrivate {
		t.Fatal("nonprivate mode accepted")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0644 {
		t.Fatal("mode repaired")
	}
}

func TestDarwinPrivateParentRejectsForeignOwner(t *testing.T) {
	dir := privateDir(t)
	if err := os.Chown(dir, os.Geteuid()+1, -1); err != nil {
		t.Skip("current uid cannot assign foreign owner to exact fixture")
	}
	defer os.Chown(dir, os.Geteuid(), -1)
	if err := CheckPrivateDirectoryParent(filepath.Join(dir, "child")); err != errPrivate {
		t.Fatal("foreign owner accepted")
	}
}

func TestDarwinReadHandleIsNotInherited(t *testing.T) {
	path := filepath.Join(privateDir(t), "state")
	if err := WriteAtomic(path, nil); err != nil {
		t.Fatal(err)
	}
	file, err := nativeReadOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("inheritable read descriptor")
	}
}
