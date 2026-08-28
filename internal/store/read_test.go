package store

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertParentDiagnostic(t *testing.T, path, wantStage, wantCategory string, wantErr error) {
	t.Helper()
	if err := CheckPrivateDirectoryParent(path); err != wantErr {
		t.Fatal("legacy parent result changed")
	}
	stage, category, err := CheckPrivateDirectoryParentDiagnostic(path)
	if err != wantErr || stage != wantStage || category != wantCategory {
		t.Fatalf("parent diagnostic stage=%q category=%q err=%v", stage, category, err)
	}
	if err != nil {
		if err.Error() != "private state unavailable" || errors.Unwrap(err) != nil {
			t.Fatal("parent public error changed")
		}
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, err), "SENSITIVE") {
				t.Fatal("parent error retained an input")
			}
		}
	}
}

func TestPrivateDirectoryParentDiagnosticPreservesChecks(t *testing.T) {
	// Empty detail, altered sentinel identity, or changed missing-leaf/unsafe
	// path behavior must fail without relying on the implementation's helpers.
	dir := privateDir(t)
	file := filepath.Join(dir, "SENSITIVE existing")
	if err := WriteAtomic(file, []byte("SENSITIVE keep")); err != nil {
		t.Fatal(err)
	}
	beforeDir, beforeFile := targetAccessSnapshot(t, dir), targetAccessSnapshot(t, file)
	for _, tc := range []struct {
		name, path, stage, category string
		err                         error
		safeMissing, safeExisting   bool
	}{
		{"missing-leaf", filepath.Join(dir, "SENSITIVE missing"), "", "", nil, true, false},
		{"existing-directory", dir, "leaf-missing", "exists", errPrivate, true, true},
		{"existing-file", file, "leaf-missing", "exists", errPrivate, true, true},
		{"missing-parent", filepath.Join(dir, "SENSITIVE missing", "child"), "ancestor-stat", "missing", errPrivate, false, false},
		{"relative", "SENSITIVE relative", "path", "rejected", errPrivate, false, false},
		{"unclean", dir + string(os.PathSeparator) + "." + string(os.PathSeparator) + "SENSITIVE leaf", "path", "rejected", errPrivate, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertParentDiagnostic(t, tc.path, tc.stage, tc.category, tc.err)
			if safePath(tc.path, true) != tc.safeMissing || safePath(tc.path, false) != tc.safeExisting {
				t.Fatal("legacy path predicate changed")
			}
		})
	}
	children, err := os.ReadDir(dir)
	kept, readErr := os.ReadFile(file)
	if err != nil || len(children) != 1 || readErr != nil || string(kept) != "SENSITIVE keep" {
		t.Fatal("parent diagnostic created or changed fixture data")
	}
	if targetAccessSnapshot(t, dir) != beforeDir || targetAccessSnapshot(t, file) != beforeFile {
		t.Fatal("parent diagnostic changed access metadata")
	}
}

func TestPrivateDirectoryParentDiagnosticDoesNotProject(t *testing.T) {
	t.Setenv("ATN_PACKAGE_DIAGNOSTICS", "1")
	dir := privateDir(t)
	sink, err := os.CreateTemp(t.TempDir(), "capture")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	func() {
		stdout, stderr := os.Stdout, os.Stderr
		os.Stdout, os.Stderr = sink, sink
		defer func() { os.Stdout, os.Stderr = stdout, stderr }()
		assertParentDiagnostic(t, dir, "leaf-missing", "exists", errPrivate)
		assertParentDiagnostic(t, filepath.Join(dir, "SENSITIVE missing"), "", "", nil)
	}()
	info, err := sink.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatal("store printed a developer diagnostic")
	}
}

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
