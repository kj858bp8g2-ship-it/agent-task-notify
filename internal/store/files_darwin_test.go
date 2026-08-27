package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// This synthetic-only probe distinguishes directory checks from native ACL
// retrieval, validation, and empty-entry semantics. No production hook is used.
func TestDarwinPrivateDirectoryNativeStages(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("fixture root unavailable")
	}
	owned := filepath.Join(root, "go-owned")
	if !safePath(owned, true) {
		t.Error("stage=go-safe-before result=0")
	} else {
		t.Log("stage=go-safe-before result=1")
	}
	if nativeMkdir(owned) != nil {
		t.Error("stage=go-mkdir result=0")
	} else {
		t.Log("stage=go-mkdir result=1")
	}
	if !safePath(owned, false) {
		t.Error("stage=go-safe-after result=0")
	} else {
		t.Log("stage=go-safe-after result=1")
	}
	if !nativePrivate(owned, true) {
		t.Error("stage=go-private result=0")
	} else {
		t.Log("stage=go-private result=1")
	}
	source := filepath.Join(root, "probe.c")
	if os.WriteFile(source, []byte(darwinACLProbeSource), 0600) != nil {
		t.Fatal("probe source unavailable")
	}
	binary := filepath.Join(root, "probe")
	compileCtx, cancelCompile := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCompile()
	if exec.CommandContext(compileCtx, "/usr/bin/clang", "-Wall", "-Wextra", "-Werror", source, "-o", binary).Run() != nil {
		t.Fatal("stage=probe-compile result=0")
	}
	runCtx, cancelRun := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelRun()
	out, runErr := exec.CommandContext(runCtx, binary, filepath.Join(root, "c-owned")).Output()
	// Even test diagnostics may emit only the fixed stage/numeric schema.
	if !regexp.MustCompile(`\A(?:stage=[a-z-]+ result=-?[0-9]+ errno=[0-9]+\n)+\z`).Match(out) {
		t.Fatal("probe output schema rejected")
	}
	t.Logf("%s", out)
	if runErr != nil {
		t.Error("stage=probe-run result=0")
	}
}

// Apple Libc posix1e/acl_entry.c documents the implementation's 0 entry /
// -1 EINVAL end behavior; posix1e/acl_file.c uses fstatx_np/lstatx_np for retrieval.
// Raw observations are intentionally collected before any production change.
const darwinACLProbeSource = `
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/acl.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>

static void emit(const char *stage, int result, int saved_errno) {
    printf("stage=%s result=%d errno=%d\n", stage, result, saved_errno);
}
static int inspect(acl_t acl, const char *valid_stage, const char *entry_stage) {
    if (acl == NULL) return 0;
    errno = 0;
    int valid = acl_valid(acl), saved = errno;
    emit(valid_stage, valid, saved);
    if (valid != 0) { acl_free(acl); return 0; }
    acl_entry_t entry;
    errno = 0;
    int result = acl_get_entry(acl, ACL_FIRST_ENTRY, &entry);
    saved = errno;
    emit(entry_stage, result, saved);
    acl_free(acl);
    return result == -1 && saved == EINVAL;
}
int main(int argc, char **argv) {
    if (argc != 2) return 2;
    errno = 0;
    int result = mkdir(argv[1], 0700), saved = errno;
    emit("mkdir", result, saved);
    if (result != 0) return 1;
    errno = 0;
    int fd = open(argv[1], O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    saved = errno;
    emit("open", fd >= 0, saved);
    if (fd < 0) return 1;
    struct stat st;
    errno = 0;
    result = fstat(fd, &st); saved = errno;
    emit("fstat", result, saved);
    int safe = result == 0 && st.st_uid == geteuid() && S_ISDIR(st.st_mode);
    emit("owned-directory", safe, 0);
    if (!safe) { close(fd); return 1; }
    errno = 0;
    acl_t acl = acl_init(0); saved = errno;
    emit("init", acl != NULL, saved);
    if (acl == NULL) { close(fd); return 1; }
    errno = 0;
    result = acl_set_fd_np(fd, acl, ACL_TYPE_EXTENDED); saved = errno;
    emit("set-fd", result, saved);
    acl_free(acl);
    int ok = result == 0;
    errno = 0;
    acl = acl_get_fd_np(fd, ACL_TYPE_EXTENDED); saved = errno;
    emit("get-fd", acl != NULL, saved);
    ok = inspect(acl, "valid-fd", "entry-fd") && ok;
    errno = 0;
    acl = acl_get_link_np(argv[1], ACL_TYPE_EXTENDED); saved = errno;
    emit("get-link", acl != NULL, saved);
    ok = inspect(acl, "valid-link", "entry-link") && ok;
    errno = 0;
    result = fchmod(fd, 0700); saved = errno;
    emit("chmod", result, saved);
    ok = result == 0 && ok;
    close(fd);
    return ok ? 0 : 1;
}
`

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
