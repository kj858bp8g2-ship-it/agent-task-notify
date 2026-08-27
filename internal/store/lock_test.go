package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLockHelper(t *testing.T) {
	mode := os.Getenv("NOTIFY_LOCK_HELPER")
	if mode == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	release, err := Acquire(ctx, os.Getenv("NOTIFY_LOCK_PATH"))
	if mode == "blocked" {
		if !errors.Is(err, context.DeadlineExceeded) {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if err != nil {
		os.Exit(3)
	}
	if mode == "release" {
		if release() != nil {
			os.Exit(4)
		}
	}
	os.Exit(0) // mode exit intentionally leaves the native lock held.
}

func TestLockIndependentProcesses(t *testing.T) {
	path := filepath.Join(privateDir(t), "lock")
	child := func(mode string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelper$")
		cmd.Env = append(os.Environ(), "NOTIFY_LOCK_HELPER="+mode, "NOTIFY_LOCK_PATH="+path)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child %s: %v %s", mode, err, out)
		}
	}
	release, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	child("blocked")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if other, err := Acquire(ctx, path); err == nil {
		other()
		t.Fatal("lock admitted second owner")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal("release must be idempotent")
	}
	child("release")
	child("exit")
	child("release")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("lock file must remain")
	}
}

func TestLockCanceledAndUnsafe(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "lock")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := Acquire(ctx, path); err == nil {
		release()
		t.Fatal("canceled context acquired")
	}
	target := filepath.Join(dir, "target")
	if err := WriteAtomic(target, []byte("safe")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err == nil {
		if release, err := Acquire(context.Background(), link); err == nil {
			release()
			t.Fatal("symlink lock accepted")
		}
	}
}
