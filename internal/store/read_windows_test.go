package store

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

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
