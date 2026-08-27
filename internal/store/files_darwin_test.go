package store

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestDarwinRejectsIncompleteFileSecurity(t *testing.T) {
	// Compile the production preamble itself: copying its predicate into a
	// probe would leave this regression test independent of the actual gate.
	parsed, err := parser.ParseFile(token.NewFileSet(), "acl_darwin.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal("production ACL source unavailable")
	}
	var preamble string
	for _, declaration := range parsed.Decls {
		imports, ok := declaration.(*ast.GenDecl)
		if !ok || imports.Tok != token.IMPORT || imports.Doc == nil {
			continue
		}
		for _, spec := range imports.Specs {
			if spec.(*ast.ImportSpec).Path.Value == `"C"` {
				preamble = imports.Doc.Text()
			}
		}
	}
	if preamble == "" {
		t.Fatal("production ACL preamble unavailable")
	}
	root := t.TempDir()
	source := filepath.Join(root, "filesec.c")
	if os.WriteFile(source, []byte(preamble+darwinIncompleteFileSecuritySource), 0600) != nil {
		t.Fatal("filesec fixture source unavailable")
	}
	binary := filepath.Join(root, "filesec")
	compileCtx, cancelCompile := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCompile()
	// The complete preamble also contains other private, unused entry points.
	if exec.CommandContext(compileCtx, "/usr/bin/clang", "-Wall", "-Wextra", "-Werror", "-Wno-unused-function", source, "-o", binary).Run() != nil {
		t.Fatal("stage=filesec-compile result=0")
	}
	runCtx, cancelRun := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelRun()
	out, runErr := exec.CommandContext(runCtx, binary).Output()
	if !regexp.MustCompile(`\A(?:stage=(?:unfilled|owner-only|group-only|mode-only|owner-group|owner-mode|group-mode|complete) result=[01]\n)+\z`).Match(out) {
		t.Fatal("filesec output schema rejected")
	}
	t.Logf("%s", out)
	if runErr != nil {
		t.Fatal("stage=filesec-run result=0")
	}
	const want = "stage=unfilled result=0\n" +
		"stage=owner-only result=0\n" +
		"stage=group-only result=0\n" +
		"stage=mode-only result=0\n" +
		"stage=owner-group result=0\n" +
		"stage=owner-mode result=0\n" +
		"stage=group-mode result=0\n" +
		"stage=complete result=1\n"
	if string(out) != want {
		t.Fatal("incomplete filesec accepted or complete no-ACL filesec rejected")
	}
}

// Missing any mandatory stat property must fail closed, even though an absent
// ACL property alone is valid after a fully populated successful statx call.
const darwinIncompleteFileSecuritySource = `
#include <stdio.h>
#include <unistd.h>

int main(void) {
    const struct { const char *stage; unsigned fields; } cases[] = {
        {"unfilled", 0}, {"owner-only", 1}, {"group-only", 2}, {"mode-only", 4},
        {"owner-group", 3}, {"owner-mode", 5}, {"group-mode", 6}, {"complete", 7}
    };
    uid_t owner = geteuid();
    gid_t group = getegid();
    mode_t mode = S_IFREG | 0600;
    for (size_t i = 0; i < sizeof(cases) / sizeof(cases[0]); i++) {
        filesec_t security = filesec_init();
        if (security == NULL) return 1;
        unsigned fields = cases[i].fields;
        if (((fields & 1) && filesec_set_property(security, FILESEC_OWNER, &owner) != 0) ||
            ((fields & 2) && filesec_set_property(security, FILESEC_GROUP, &group) != 0) ||
            ((fields & 4) && filesec_set_property(security, FILESEC_MODE, &mode) != 0)) {
            filesec_free(security);
            return 1;
        }
        int result = notify_filesec_empty(security);
        filesec_free(security);
        printf("stage=%s result=%d\n", cases[i].stage, result);
    }
    return 0;
}
`

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
	if !regexp.MustCompile(`\A(?:stage=(?:mkdir|open|fstat|owner|type|init|set-fd|get-fd|valid-fd|entry-fd|get-link|valid-link|entry-link|statx-fd|statx-link|query-fd|query-link|present-fd|present-link|chmod) result=-?[0-9]+ errno=[0-9]+\n)+\z`).Match(out) {
		t.Fatal("probe output schema rejected")
	}
	t.Logf("%s", out)
	if runErr != nil {
		t.Error("stage=probe-run result=0")
	}
}

// Apple Libc posix1e/acl_entry.c documents the implementation's 0 entry /
// -1 EINVAL end behavior; posix1e/acl_file.c uses fstatx_np/lstatx_np for retrieval.
// Keep raw getter observations, but assert the explicit successful extended-stat
// and property-query boundary rather than interpreting NULL/errno in isolation.
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
static int inspect_security(filesec_t security, const char *query_stage,
    const char *present_stage, const char *valid_stage, const char *entry_stage) {
    int present = 0;
    errno = 0;
    int result = filesec_query_property(security, FILESEC_ACL, &present), saved = errno;
    emit(query_stage, result, saved);
    if (result != 0) return 0;
    emit(present_stage, present != 0, 0);
    if (!present) return 1;
    acl_t acl = NULL;
    if (filesec_get_property(security, FILESEC_ACL, &acl) != 0) return 0;
    return inspect(acl, valid_stage, entry_stage);
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
    int owner = result == 0 && st.st_uid == geteuid();
    int type = result == 0 && S_ISDIR(st.st_mode);
    emit("owner", owner, 0);
    emit("type", type, 0);
    int safe = owner && type;
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
    if (acl != NULL) acl_free(acl);
    errno = 0;
    acl = acl_get_link_np(argv[1], ACL_TYPE_EXTENDED); saved = errno;
    emit("get-link", acl != NULL, saved);
    if (acl != NULL) acl_free(acl);
    filesec_t security = filesec_init();
    if (security == NULL) { close(fd); return 1; }
    errno = 0;
    result = fstatx_np(fd, &st, security); saved = errno;
    emit("statx-fd", result, saved);
    ok = (result == 0 && inspect_security(security, "query-fd", "present-fd",
        "valid-fd", "entry-fd")) && ok;
    filesec_free(security);
    security = filesec_init();
    if (security == NULL) { close(fd); return 1; }
    errno = 0;
    result = lstatx_np(argv[1], &st, security); saved = errno;
    emit("statx-link", result, saved);
    ok = (result == 0 && inspect_security(security, "query-link", "present-link",
        "valid-link", "entry-link")) && ok;
    filesec_free(security);
    errno = 0;
    result = fchmod(fd, 0700); saved = errno;
    emit("chmod", result, saved);
    ok = result == 0 && ok;
    close(fd);
    return ok ? 0 : 1;
}
`

func TestDarwinACLAbsentPresentAndReadFailure(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("fixture root unavailable")
	}
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name)
			if directory {
				err = os.Mkdir(path, 0700)
			} else {
				err = os.WriteFile(path, nil, 0600)
			}
			if err != nil {
				t.Fatal("fixture creation failed")
			}
			// Remove any inherited ACL from this test-created object only.
			if exec.Command("/bin/chmod", "-N", path).Run() != nil {
				t.Fatal("fixture ACL removal failed")
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal("fixture open failed")
			}
			defer file.Close()
			if !emptyPathACL(path) || !emptyFileACL(int(file.Fd())) {
				t.Fatal("successful no-ACL query rejected")
			}
			addDarwinFixtureACL(t, path, false)
			before := darwinACLListing(t, path)
			if emptyPathACL(path) || emptyFileACL(int(file.Fd())) {
				t.Fatal("nonempty ACL accepted")
			}
			if darwinACLListing(t, path) != before {
				t.Fatal("ACL inspection changed fixture")
			}
		})
	}
	if emptyFileACL(-1) || clearNewFileACL(-1) {
		t.Fatal("invalid descriptor accepted")
	}
	if emptyPathACL(filepath.Join(root, "missing")) {
		t.Fatal("missing path mistaken for absent ACL")
	}
	if exec.Command("/bin/chmod", "-N", filepath.Join(root, "file")).Run() != nil {
		t.Fatal("fixture ACL removal failed")
	}
	if !emptyPathACL(filepath.Join(root, "file")) {
		t.Fatal("symlink target fixture is not ACL-free")
	}
	link := filepath.Join(root, "link")
	if os.Symlink(filepath.Join(root, "file"), link) != nil {
		t.Fatal("fixture symlink failed")
	}
	if emptyPathACL(link) {
		t.Fatal("symlink accepted")
	}
}

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
