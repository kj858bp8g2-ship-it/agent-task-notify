package store

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func privateDir(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "private")
	if err := EnsurePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Catches in-place truncation and partial replacement observable by readers.
func TestAtomicReplacement(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "state")
	old, next := bytes.Repeat([]byte("a"), 32768), bytes.Repeat([]byte("b"), 32768)
	if err := WriteAtomic(path, old); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	failures := make(chan error, 1)
	reads := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			got, err := readForReplacement(path)
			reads++
			if err != nil || (!bytes.Equal(got, old) && !bytes.Equal(got, next)) {
				select {
				case failures <- err:
				default:
				}
				return
			}
		}
	}()
	for i := 0; i < 30; i++ {
		data := next
		if i%2 == 1 {
			data = old
		}
		if err := WriteAtomic(path, data); err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if reads == 0 {
		t.Fatal("concurrent reader never ran")
	}
	select {
	case err := <-failures:
		t.Fatalf("reader saw missing or partial state: %v", err)
	default:
	}
	if err := WriteAtomic(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "second" {
		t.Fatal("replacement failed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("temporary file leaked")
	}
}

func TestPrivateFilesRejectUnsafeTargets(t *testing.T) {
	dir := privateDir(t)
	if err := WriteAtomic(filepath.Join(dir, "missing", "state"), []byte("x")); err == nil {
		t.Fatal("missing parent accepted")
	}
	target := filepath.Join(dir, "original")
	if err := WriteAtomic(target, []byte("original")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		t.Logf("symlink unavailable: %v", err)
	} else {
		if err := WriteAtomic(link, []byte("overwrite")); err == nil {
			t.Fatal("symlink accepted")
		}
		got, _ := os.ReadFile(target)
		if string(got) != "original" {
			t.Fatal("symlink target overwritten")
		}
	}
	sub := filepath.Join(dir, "directory")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(sub, []byte("x")); err == nil {
		t.Fatal("directory overwritten")
	}
	if stat, err := os.Stat(sub); err != nil || !stat.IsDir() {
		t.Fatal("failed replacement damaged original")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if len(e.Name()) > 5 && e.Name()[:5] == ".tmp-" {
			t.Fatal("failed write leaked temporary file")
		}
	}
}
