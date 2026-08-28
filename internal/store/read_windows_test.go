package store

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsDirectoryParentDiagnosticJunction(t *testing.T) {
	// Go's current mount-point mode is irregular, so the actual native
	// reparse predicate, not a symlink-only check, must reject this fixture.
	t.Setenv("GODEBUG", os.Getenv("GODEBUG")+",winsymlink=1")
	dir := privateDir(t)
	target := filepath.Join(dir, "target")
	if err := EnsurePrivateDirectory(target); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(target, "state")
	if err := WriteAtomic(state, []byte("SENSITIVE keep")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "SENSITIVE junction")
	makeJunction(t, link, target)
	before := targetAccessSnapshot(t, target)
	for _, path := range []string{link, filepath.Join(link, "missing"), filepath.Join(link, "state")} {
		assertParentDiagnostic(t, path, "reparse-point", "rejected", errPrivate)
	}
	children, err := os.ReadDir(target)
	data, readErr := os.ReadFile(state)
	if err != nil || len(children) != 1 || readErr != nil || string(data) != "SENSITIVE keep" || targetAccessSnapshot(t, target) != before {
		t.Fatal("junction diagnostic changed its target")
	}
}

func TestWindowsDirectoryParentDiagnosticNonDirectory(t *testing.T) {
	dir := privateDir(t)
	file := filepath.Join(dir, "SENSITIVE file")
	if err := WriteAtomic(file, []byte("keep")); err != nil {
		t.Fatal(err)
	}
	assertParentDiagnostic(t, filepath.Join(file, "child"), "ancestor-directory", "rejected", errPrivate)
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep" {
		t.Fatal("non-directory parent changed")
	}
}

func TestWindowsParentDiagnosticNativeCategories(t *testing.T) {
	// Classification only: these synthetic errors do not reproduce an actual
	// CI owner/token/attribute query failure.
	for _, tc := range []struct {
		err  error
		want string
	}{
		{windows.ERROR_SHARING_VIOLATION, "sharing"},
		{windows.ERROR_LOCK_VIOLATION, "sharing"},
		{windows.ERROR_ACCESS_DENIED, "access"},
		{windows.ERROR_FILE_NOT_FOUND, "missing"},
		{windows.ERROR_PATH_NOT_FOUND, "missing"},
		{windows.ERROR_FILE_EXISTS, "exists"},
		{windows.ERROR_ALREADY_EXISTS, "exists"},
		{windows.ERROR_DISK_FULL, "io"},
		{windows.ERROR_WRITE_FAULT, "io"},
		{windows.ERROR_READ_FAULT, "io"},
		{windows.ERROR_INVALID_PARAMETER, "other"},
	} {
		wrapped := &os.PathError{Op: "SENSITIVE operation", Path: "SENSITIVE path", Err: tc.err}
		if nativeErrorCategory(wrapped) != tc.want {
			t.Fatal("native diagnostic category changed or exposed input")
		}
	}
}

func TestWindowsPrivateReadRejectsReparseAncestors(t *testing.T) {
	dir := privateDir(t)
	target := filepath.Join(dir, "target")
	if err := EnsurePrivateDirectory(target); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(target, "state")
	if err := WriteAtomic(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "junction")
	makeJunction(t, link, target)
	if err := CheckPrivateDirectoryParent(filepath.Join(link, "missing")); err != errPrivate {
		t.Fatal("linked parent accepted")
	}
	for _, candidate := range []string{link, filepath.Join(link, "state")} {
		if _, err := ReadPrivate(candidate, 1024); err != errPrivate {
			t.Fatal("reparse read accepted")
		}
		if err := RemovePrivate(candidate); err != errPrivate {
			t.Fatal("reparse removal accepted")
		}
	}
	if err := CheckPrivateDirectory(link); err != errPrivate {
		t.Fatal("reparse directory accepted")
	}
	if got, _ := os.ReadFile(path); string(got) != "original" {
		t.Fatal("reparse target changed")
	}
}

func TestWindowsPrivateParentRejectsForeignOwner(t *testing.T) {
	dir := privateDir(t)
	foreign, err := windows.StringToSid("S-1-5-21-111-222-333-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, foreign, nil, nil, nil); err != nil {
		t.Skip("current token cannot assign foreign owner to exact fixture")
	}
	if err := CheckPrivateDirectoryParent(filepath.Join(dir, "child")); err != errPrivate {
		t.Fatal("foreign owner accepted")
	}
	before := targetAccessSnapshot(t, dir)
	assertParentDiagnostic(t, filepath.Join(dir, "child"), "ancestor-owner", "rejected", errPrivate)
	if targetAccessSnapshot(t, dir) != before {
		t.Fatal("foreign owner repaired")
	}
}

func TestWindowsReadHandleIsNotInherited(t *testing.T) {
	path := filepath.Join(privateDir(t), "state")
	if err := WriteAtomic(path, nil); err != nil {
		t.Fatal(err)
	}
	file, err := nativeReadOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var flags uint32
	ok, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetHandleInformation").Call(file.Fd(), uintptr(unsafe.Pointer(&flags)))
	if ok == 0 || flags&windows.HANDLE_FLAG_INHERIT != 0 {
		t.Fatal("inheritable read handle")
	}
}
