package store

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateDirectoryParentChecksOnlyMissingLeaf(t *testing.T) {
	dir := privateDir(t)
	missing := filepath.Join(dir, "missing")
	if err := CheckPrivateDirectoryParent(missing); err != nil {
		t.Fatal("safe parent rejected")
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatal("parent check created leaf")
	}
	if err := CheckPrivateDirectoryParent(dir); err != errPrivate {
		t.Fatal("existing leaf accepted")
	}
	if err := CheckPrivateDirectoryParent(filepath.Join(missing, "child")); err != errPrivate {
		t.Fatal("missing direct parent accepted")
	}
}

func TestMissingReadDoesNotCreate(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "absent.json")
	_, err := ReadPrivate(path, 1024)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("wrong missing result")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("read wrote a file")
	}
	if err := CheckPrivateDirectory(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("missing directory accepted")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("checks created state")
	}
}

func TestPrivateReadBoundsAndRemoval(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "state")
	if err := WriteAtomic(path, []byte("abcd")); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int64{0, -1, 3, math.MaxInt64} {
		if _, err := ReadPrivate(path, limit); err != errPrivate {
			t.Fatal("unsafe size accepted")
		}
	}
	got, err := ReadPrivate(path, 4)
	if err != nil || string(got) != "abcd" {
		t.Fatal("exact bound rejected")
	}
	if err := RemovePrivate(path); err != nil {
		t.Fatal(err)
	}
	if err := RemovePrivate(path); err != ErrNotFound {
		t.Fatal("missing removal not distinct")
	}
	if err := RemovePrivate(dir); err != errPrivate {
		t.Fatal("removed directory")
	}
	if _, err := ReadPrivate(dir, 100); err != errPrivate {
		t.Fatal("read directory")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("directory changed")
	}
}

func TestPrivateReadsNeverRepairHostMetadata(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "hostile")
	if err := WriteAtomic(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	makeTargetNonPrivate(t, path)
	before := targetAccessSnapshot(t, path)
	if _, err := ReadPrivate(path, 1024); err != errPrivate {
		t.Fatal("nonprivate read accepted")
	}
	if err := RemovePrivate(path); err != errPrivate {
		t.Fatal("nonprivate removal accepted")
	}
	if after := targetAccessSnapshot(t, path); after != before {
		t.Fatal("metadata repaired")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "original" {
		t.Fatal("hostile file changed")
	}
	makeTargetNonPrivate(t, dir)
	before = targetAccessSnapshot(t, dir)
	if err := CheckPrivateDirectory(dir); err != errPrivate {
		t.Fatal("nonprivate directory accepted")
	}
	if after := targetAccessSnapshot(t, dir); after != before {
		t.Fatal("directory repaired")
	}
}

// A validated old handle must not authorize reading/removing a replacement name.
func TestPrivateReadRechecksOpenedIdentity(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "state")
	if err := WriteAtomic(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	file, err := nativeReadOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := WriteAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if samePrivateFile(file, path) {
		t.Fatal("stale handle accepted replacement name")
	}
	if _, err := file.Write([]byte("bad")); err == nil {
		t.Fatal("read handle writable")
	}
	if got, err := ReadPrivate(path, 3); err != nil || string(got) != "new" {
		t.Fatal("replacement not readable")
	}
}
