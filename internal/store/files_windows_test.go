package store

import (
	"context"
	"encoding/binary"
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

func readForReplacement(path string) ([]byte, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(h), path)
	defer file.Close()
	return io.ReadAll(file)
}

func makeTargetNonPrivate(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)(A;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func targetAccessSnapshot(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

// Independently inspect the kernel descriptor, not a production predicate.
func assertPrivateACL(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("DACL not protected")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 2 {
		t.Fatal("expected exactly two private ACEs")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{user.User.Sid.String(): false, "S-1-5-18": false}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if _, ok := expected[sid]; !ok || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask != 0x1f01ff {
			t.Fatal("unexpected access grant")
		}
		expected[sid] = true
	}
	for _, found := range expected {
		if !found {
			t.Fatal("required principal missing")
		}
	}
}

func TestWindowsProtectedPrivateACL(t *testing.T) {
	dir := privateDir(t)
	assertPrivateACL(t, dir)
	path := filepath.Join(dir, "state")
	if err := WriteAtomic(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	assertPrivateACL(t, path)
	if err := EnsurePrivateDirectory(dir); err != nil {
		t.Fatal("existing private dir rejected")
	}
}

func TestWindowsDoesNotSecureUnrelatedDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	before := sd.String()
	if err := EnsurePrivateDirectory(dir); err == nil {
		t.Fatal("unprotected unrelated directory accepted")
	}
	sd, err = windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || sd.String() != before {
		t.Fatal("unrelated DACL modified")
	}
}

func TestWindowsRejectsJunctions(t *testing.T) {
	dir := privateDir(t)
	target := filepath.Join(dir, "target")
	if err := EnsurePrivateDirectory(target); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(target, "state")
	if err := WriteAtomic(state, []byte("original")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "junction")
	makeJunction(t, link, target)
	if err := EnsurePrivateDirectory(link); err == nil {
		t.Fatal("junction directory accepted")
	}
	if err := WriteAtomic(link, []byte("bad")); err == nil {
		t.Fatal("junction leaf accepted")
	}
	if err := WriteAtomic(filepath.Join(link, "state"), []byte("bad")); err == nil {
		t.Fatal("junction ancestor accepted")
	}
	if release, err := Acquire(context.Background(), filepath.Join(link, "lock")); err == nil {
		release()
		t.Fatal("junction lock accepted")
	}
	got, err := os.ReadFile(state)
	if err != nil || string(got) != "original" {
		t.Fatal("junction target modified")
	}
}

// A native directory junction needs no symlink privilege and exercises an actual
// reparse object, not a mocked attribute result.
func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Mkdir(link, 0700); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(link)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	substitute, err := windows.UTF16FromString(`\??\` + target)
	if err != nil {
		t.Fatal(err)
	}
	printName, err := windows.UTF16FromString(target)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 16+2*(len(substitute)+len(printName)))
	binary.LittleEndian.PutUint32(data, windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(data[4:], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:], uint16((len(substitute)-1)*2))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(data[14:], uint16((len(printName)-1)*2))
	for i, v := range append(substitute, printName...) {
		binary.LittleEndian.PutUint16(data[16+i*2:], v)
	}
	var returned uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &returned, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsBlockedReplacementPreservesOriginal(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "state")
	if err := WriteAtomic(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	started := time.Now()
	if err := WriteAtomic(path, []byte("replacement")); err == nil {
		t.Fatal("non-delete-sharing reader did not block replacement")
	} else {
		var detail *privateStateError
		if !errors.As(err, &detail) {
			t.Fatal("missing replacement stage diagnostic")
		}
		if detail.stage != "replace" || (detail.category != "sharing" && detail.category != "access") {
			t.Fatalf("wrong blocked replacement classification: %s/%s", detail.stage, detail.category)
		}
		logWriteFailure(t, err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("replacement retry unbounded")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original" {
		t.Fatal("failed replacement modified original")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("failed replacement leaked temporary")
	}
}

// A held delete-sharing handle isolates rename semantics from rapid reader
// open/close timing. The old handle must retain old data while the name advances.
func TestWindowsReplacementWithHeldDeleteSharingReader(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "状态 空间😀")
	if err := WriteAtomic(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	reader := os.NewFile(uintptr(h), path)
	defer reader.Close()
	for i := 0; i < 3; i++ {
		if err := WriteAtomic(path, []byte("replacement")); err != nil {
			logWriteFailure(t, err)
			t.Fatal("held delete-sharing reader prevented replacement")
		}
		assertPrivateACL(t, path)
	}
	old, err := io.ReadAll(reader)
	if err != nil || string(old) != "original" {
		t.Fatal("old handle did not retain old complete data")
	}
	current, err := readForReplacement(path)
	if err != nil || string(current) != "replacement" {
		t.Fatal("current name did not expose new complete data")
	}
}

func TestWindowsReplacementDoesNotBypassReadOnly(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "readonly")
	if err := WriteAtomic(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if err := WriteAtomic(path, []byte("replacement")); err == nil {
		t.Fatal("read-only target replaced")
	} else {
		logWriteFailure(t, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original" {
		t.Fatal("read-only contents changed")
	}
	name, _ := windows.UTF16PtrFromString(path)
	attrs, err := windows.GetFileAttributes(name)
	if err != nil || attrs&windows.FILE_ATTRIBUTE_READONLY == 0 {
		t.Fatal("read-only attribute changed")
	}
	assertPrivateACL(t, path)
}
