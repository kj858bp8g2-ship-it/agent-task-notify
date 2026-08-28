package hostfile

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func predictionDetails(s Snapshot) string { return "native=darwin" }

func hostDirectory(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "host")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	if unix.Lstat(path, &st) != nil || st.Uid != uint32(os.Geteuid()) {
		t.Fatal("fixture owner")
	}
	return path
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func fixtureACL(t *testing.T, path, entry string) {
	t.Helper()
	if err := exec.Command("/bin/chmod", "+a", entry, path).Run(); err != nil {
		t.Fatal("fixture ACL setup", err)
	}
}

func aclListing(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("/bin/ls", "-lde", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.Join(lines[1:], "\n")
}

func accessListing(t *testing.T, path string) string {
	t.Helper()
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%d/%d/%o/%x/%s", st.Uid, st.Gid, st.Mode, st.Flags, aclListing(t, path))
}

func changeAccess(t *testing.T, path string) {
	t.Helper()
	fixtureACL(t, path, "everyone allow read,readattr,readextattr,readsecurity")
}

func assertOwnerOnly(t *testing.T, path string) {
	t.Helper()
	var st unix.Stat_t
	if unix.Lstat(path, &st) != nil || st.Mode != unix.S_IFREG|0600 || st.Uid != uint32(os.Geteuid()) || aclListing(t, path) != "" {
		t.Fatal("not owner-only")
	}
}

func TestDarwinExtendedACLOwnerModeAndFlagsPreserved(t *testing.T) {
	path := fixture(t)
	changeAccess(t, path)
	if aclListing(t, path) == "" {
		t.Fatal("real extended ACL missing")
	}
	if err := unix.Chflags(path, unix.UF_HIDDEN); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, path)
	candidates, err := before.ExpectedAccessDigests(true)
	if err != nil || len(candidates) != 1 {
		t.Fatal("ACL/flags prediction", err)
	}
	access := accessListing(t, path)
	if err := Replace(path, before, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	assertData(t, path, "replacement")
	if accessListing(t, path) != access {
		t.Fatal("ACL/owner/group/mode/flags not preserved")
	}
	digest, err := snapshot(t, path).AccessDigest()
	if err != nil || digest != candidates[0] {
		t.Fatal("ACL/flags digest changed")
	}
}

func TestDarwinRestrictiveFlagsAndDenyACLNeverCleared(t *testing.T) {
	for _, kind := range []string{"immutable", "append", "deny-write"} {
		t.Run(kind, func(t *testing.T) {
			path := fixture(t)
			switch kind {
			case "immutable":
				if err := unix.Chflags(path, unix.UF_IMMUTABLE); err != nil {
					t.Fatal(err)
				}
			case "append":
				if err := unix.Chflags(path, unix.UF_APPEND); err != nil {
					t.Fatal(err)
				}
			case "deny-write":
				fixtureACL(t, path, "everyone deny write,append,delete")
			}
			t.Cleanup(func() { _ = unix.Chflags(path, 0); _ = exec.Command("/bin/chmod", "-N", path).Run() })
			before := snapshot(t, path)
			access := accessListing(t, path)
			if Replace(path, before, []byte("bad")) == nil || Remove(path, before) == nil {
				t.Fatal("restriction bypassed")
			}
			assertData(t, path, `{"other":1}`)
			if accessListing(t, path) != access {
				t.Fatal("restriction cleared")
			}
		})
	}
}

func TestDarwinRejectsLinksAndFIFO(t *testing.T) {
	path := fixture(t)
	link := filepath.Join(hostDirectory(t), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(link, 4096); err == nil {
		t.Fatal("symlink accepted")
	}
	ancestor := filepath.Join(hostDirectory(t), "ancestor")
	if err := os.Symlink(filepath.Dir(path), ancestor); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(filepath.Join(ancestor, filepath.Base(path)), 4096); err == nil {
		t.Fatal("linked ancestor accepted")
	}
	fifo := filepath.Join(hostDirectory(t), "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(fifo, 4096); err == nil {
		t.Fatal("FIFO accepted")
	}
	assertData(t, path, `{"other":1}`)
}

func TestDarwinNewFileDoesNotInheritHostACL(t *testing.T) {
	dir := hostDirectory(t)
	fixtureACL(t, dir, "everyone allow read,readattr,readextattr,readsecurity,file_inherit,directory_inherit")
	parent := accessListing(t, dir)
	path := filepath.Join(dir, "new")
	temp, _, err := prepareTemporary(path, nil, nativeAccess{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := temp.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatal("private temporary not empty")
	}
	assertOwnerOnly(t, temp.Name())
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Replace(path, snapshot(t, path), []byte("new")); err != nil {
		t.Fatal(err)
	}
	assertOwnerOnly(t, path)
	if accessListing(t, dir) != parent {
		t.Fatal("Agent directory ACL changed")
	}
}

func TestDarwinForeignOwnerRejectedWhenFixturePermitted(t *testing.T) {
	if !ownedUID(uint32(os.Geteuid())) || ownedUID(uint32(os.Geteuid()+1)) {
		t.Fatal("foreign owner policy")
	}
	path := fixture(t)
	if err := os.Chown(path, os.Geteuid()+1, -1); err != nil {
		t.Skip("foreign-owner fixture unavailable without additional privileges")
	}
	if _, err := Read(path, 4096); err == nil {
		t.Fatal("foreign owner accepted")
	}
}

func TestDarwinExtendedACLCopiedBeforeAnyBytes(t *testing.T) {
	path := fixture(t)
	changeAccess(t, path)
	before := snapshot(t, path)
	source, err := nativeOpen(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	temp, _, err := prepareTemporary(path, source, before.state.access)
	if err != nil {
		t.Fatal(err)
	}
	defer temp.Close()
	info, err := temp.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatal("temporary contains data before inspection")
	}
	if aclListing(t, path) == "" || accessListing(t, temp.Name()) != accessListing(t, path) {
		t.Fatal("real extended ACL missing before first write")
	}
}

func TestDarwinNewAccessPredictionUsesCapturedParentGroup(t *testing.T) {
	dir := hostDirectory(t)
	groups, err := os.Getgroups()
	if err != nil || len(groups) == 0 {
		t.Fatal("fixture groups unavailable")
	}
	group := groups[0]
	for _, candidate := range groups {
		if candidate != os.Getegid() {
			group = candidate
			break
		}
	}
	if err := os.Chown(dir, -1, group); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, os.ModeSetgid|0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "new")
	before := snapshot(t, path)
	candidates, err := before.ExpectedAccessDigests(true)
	if err != nil || len(candidates) != 1 {
		t.Fatal("new group prediction")
	}
	if err := Replace(path, before, []byte("new")); err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	if unix.Lstat(path, &st) != nil || st.Gid != uint32(group) {
		t.Fatal("parent group not preserved on new file")
	}
	digest, err := snapshot(t, path).AccessDigest()
	if err != nil || digest != candidates[0] {
		t.Fatal("captured parent group not predicted")
	}
}

func TestDarwinEmptyACLFlagsAreNotDiscarded(t *testing.T) {
	path := fixture(t)
	initial, err := snapshot(t, path).AccessDigest()
	if err != nil {
		t.Fatal(err)
	}
	dir := hostDirectory(t)
	source := filepath.Join(dir, "acl-fixture.c")
	binary := filepath.Join(dir, "acl-fixture")
	if err := os.WriteFile(source, []byte(darwinEmptyACLFixture), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "/usr/bin/clang", "-Wall", "-Wextra", "-Werror", source, "-o", binary).Run(); err != nil {
		t.Fatal("fixture compile failed")
	}
	out, err := exec.CommandContext(ctx, binary, path).CombinedOutput()
	if len(out) > 2048 {
		t.Fatal("fixture diagnostic exceeded bound")
	}
	t.Log(string(out))
	if err != nil {
		t.Fatal("fixture empty ACL flag setup failed")
	}
	before := snapshot(t, path)
	flagged, err := before.AccessDigest()
	if err != nil || flagged == initial {
		t.Fatal("zero-entry ACL flags discarded")
	}
	if err := Replace(path, before, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := snapshot(t, path).AccessDigest()
	if err != nil || got != flagged {
		t.Fatal("zero-entry ACL flags changed")
	}
}

// This native fixture touches only its already-created synthetic regular file.
const darwinEmptyACLFixture = `
#include <sys/types.h>
#include <sys/acl.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <unistd.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
static void report_acl(const char *stage, int status, int saved_errno, acl_t acl, int present) {
    int count = -1, no_inherit = -1, defer_inherit = -1;
    ssize_t copied = -1;
    if (acl != NULL) {
        acl_entry_t entry;
        count = 0;
        int next = ACL_FIRST_ENTRY;
        while (count <= ACL_MAX_ENTRIES && acl_get_entry(acl, next, &entry) == 0) {
            count++;
            next = ACL_NEXT_ENTRY;
        }
        acl_flagset_t flags;
        if (acl_get_flagset_np(acl, &flags) == 0) {
            no_inherit = acl_get_flag_np(flags, ACL_FLAG_NO_INHERIT);
            defer_inherit = acl_get_flag_np(flags, ACL_FLAG_DEFER_INHERIT);
        }
        ssize_t size = acl_size(acl);
        if (size > 0 && size <= 1048576) {
            void *buffer = calloc(1, (size_t)size);
            if (buffer != NULL) { copied = acl_copy_ext(buffer, acl, size); free(buffer); }
        }
    }
    printf("stage=%s status=%d errno=%d present=%d entries=%d no-inherit=%d defer-inherit=%d serialized-length=%ld\n",
        stage, status, saved_errno, present, count, no_inherit, defer_inherit, (long)copied);
}
static void report_fd(const char *stage, int fd) {
    errno = 0;
    acl_t acl = acl_get_fd_np(fd, ACL_TYPE_EXTENDED);
    int saved_errno = errno;
    report_acl(stage, acl == NULL ? -1 : 0, saved_errno, acl, acl == NULL ? -1 : 1);
    if (acl != NULL) acl_free(acl);
}
static void report_filesec(int fd) {
    errno = 0;
    filesec_t security = filesec_init();
    int status = -1, present = -1, saved_errno = errno;
    acl_t acl = NULL;
    struct stat st;
    if (security != NULL) {
        errno = 0;
        status = fstatx_np(fd, &st, security);
        if (status == 0) { errno = 0; status = filesec_query_property(security, FILESEC_ACL, &present); }
        if (status == 0 && present) { errno = 0; status = filesec_get_property(security, FILESEC_ACL, &acl); }
        saved_errno = errno;
        filesec_free(security);
    }
    report_acl("fstatx-FILESEC_ACL", status, saved_errno, acl, present);
    if (acl != NULL) acl_free(acl);
}
int main(int argc, char **argv) {
    if (argc != 2) return 2;
    int fd = open(argv[1], O_RDWR|O_NOFOLLOW|O_CLOEXEC);
    if (fd < 0) return 3;
    struct stat st;
    if (fstat(fd, &st) != 0 || !S_ISREG(st.st_mode) || st.st_uid != geteuid()) { close(fd); return 4; }
    acl_t acl = acl_init(0);
    acl_flagset_t flags;
    if (acl == NULL) { close(fd); return 5; }
    errno = 0;
    int ok = acl_get_flagset_np(acl, &flags) == 0 &&
        acl_add_flag_np(flags, ACL_FLAG_NO_INHERIT) == 0;
    report_acl("constructed", ok ? 0 : -1, errno, acl, 1);
    errno = 0;
    ok = ok && acl_set_fd_np(fd, acl, ACL_TYPE_EXTENDED) == 0;
    int set_errno = errno;
    acl_free(acl);
    if (!ok) { report_acl("post-set-same-fd", -1, set_errno, NULL, -1); close(fd); return 6; }
    report_fd("post-set-same-fd", fd);
    report_filesec(fd);
    close(fd);
    fd = open(argv[1], O_RDONLY|O_NOFOLLOW|O_CLOEXEC);
    if (fd < 0) { report_acl("reopened", -1, errno, NULL, -1); return 7; }
    if (fstat(fd, &st) != 0 || !S_ISREG(st.st_mode) || st.st_uid != geteuid()) { close(fd); return 8; }
    report_fd("reopened", fd);
    close(fd);
    return 0;
}
`
