package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

func readForReplacement(path string) ([]byte, error) { return os.ReadFile(path) }

func makeTargetNonPrivate(t *testing.T, path string) { t.Helper(); addDarwinFixtureACL(t, path, false) }
func targetAccessSnapshot(t *testing.T, path string) string {
	t.Helper()
	return darwinACLListing(t, path)
}

func TestDarwinPrivateModes(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "state")
	if err := WriteAtomic(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0700, path: 0600} {
		stat, err := os.Stat(path)
		if err != nil || stat.Mode().Perm() != want {
			t.Fatal("incorrect private mode")
		}
	}
	if err := EnsurePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
}

func TestDarwinDoesNotSecureUnrelatedDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "existing"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateDirectory(dir); err == nil {
		t.Fatal("unrelated directory accepted")
	}
	stat, err := os.Stat(dir)
	if err != nil || stat.Mode().Perm() != 0755 {
		t.Fatal("unrelated directory changed")
	}
}

func darwinACLListing(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("/bin/ls", "-lde", path).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture ACL inspection: %v %s", err, out)
	}
	return string(out)
}

func addDarwinFixtureACL(t *testing.T, path string, inherit bool) {
	t.Helper()
	entry := "everyone allow read,readattr,readextattr,readsecurity"
	if inherit {
		entry = "everyone allow read,readattr,readextattr,readsecurity,file_inherit,directory_inherit"
	}
	out, err := exec.Command("/bin/chmod", "+a", entry, path).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture ACL setup: %v %s", err, out)
	}
}

func assertNoDarwinACL(t *testing.T, path string) {
	t.Helper()
	if regexp.MustCompile(`(?m)^\s*\d+:`).MatchString(darwinACLListing(t, path)) {
		t.Fatal("extended ACL remains")
	}
}

func TestDarwinRejectsExtendedACLWithoutModification(t *testing.T) {
	dir := privateDir(t)
	if err := os.WriteFile(filepath.Join(dir, "existing"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	addDarwinFixtureACL(t, dir, false)
	before := darwinACLListing(t, dir)
	if err := EnsurePrivateDirectory(dir); err == nil {
		t.Fatal("extended ACL directory accepted")
	}
	if err := WriteAtomic(filepath.Join(dir, "existing"), []byte("bad")); err == nil {
		t.Fatal("extended ACL parent accepted")
	}
	if got := darwinACLListing(t, dir); got != before {
		t.Fatal("unrelated ACL changed")
	}
}

func TestDarwinNewObjectsRemoveInheritedACLBeforeWrite(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	addDarwinFixtureACL(t, parent, true)
	dir := filepath.Join(parent, "owned")
	if err := EnsurePrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	assertNoDarwinACL(t, dir)
	// Exercise exclusive temp creation directly, before any data has been written.
	path := filepath.Join(parent, "new-temp")
	file, err := nativeOpen(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() != 0 || stat.Mode().Perm() != 0600 {
		t.Fatal("temp not private before write")
	}
	assertNoDarwinACL(t, path)
	if _, err := file.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
}
