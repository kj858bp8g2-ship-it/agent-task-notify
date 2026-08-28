package hostfile

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func fixtureDescriptor(t *testing.T, dacl string, inherit bool) *windows.SECURITY_DESCRIPTOR {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	group, err := windows.GetCurrentProcessToken().GetTokenPrimaryGroup()
	if err != nil {
		t.Fatal(err)
	}
	flags := ""
	if inherit {
		flags = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "G:" + group.PrimaryGroup.String() + dacl + "(A;" + flags + ";FA;;;" + user.User.Sid.String() + ")(A;" + flags + ";FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	return sd
}

// A new explicitly owned child is needed on CI where TokenOwner is a group.
// No ancestor or pre-existing object is changed to make this fixture pass.
func hostDirectory(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "owned-host")
	sd := fixtureDescriptor(t, "D:P", true)
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, _ := windows.UTF16PtrFromString(path)
	if err := windows.CreateDirectory(name, &sa); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(sd)
	assertCurrentOwner(t, path)
	return path
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	sd := fixtureDescriptor(t, "D:P", false)
	writeFixtureDescriptor(t, path, data, sd)
}

func writeFixtureDescriptor(t *testing.T, path string, data []byte, sd *windows.SECURITY_DESCRIPTOR) {
	t.Helper()
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	runtime.KeepAlive(sd)
	if err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(h), path)
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertCurrentOwner(t, path)
}

func TestWindowsUnprotectedInheritedACLAndDigestPreserved(t *testing.T) {
	path := filepath.Join(hostDirectory(t), "inherited.json")
	writeFixtureDescriptor(t, path, []byte("old"), fixtureDescriptor(t, "D:AI", false))
	// Initialize only this new fixture through the OS inheritance API. Raw
	// CreateFile does not preserve the requested AI control bit on this host.
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, path)
	if before.state.access.control&windows.SE_DACL_PROTECTED != 0 {
		t.Fatal("fixture unexpectedly protected")
	}
	access := accessListing(t, path)
	candidates, err := before.ExpectedAccessDigests(true)
	if err != nil || len(candidates) != 1 {
		t.Fatal("unprotected candidate prediction")
	}
	source, err := nativeOpen(path, true)
	if err != nil {
		t.Fatal("probe source", err)
	}
	defer source.Close()
	temp, err := nativeCreate(filepath.Join(filepath.Dir(path), "probe-temp"))
	if err != nil {
		t.Fatal(err)
	}
	defer temp.Close()
	if err := nativeCopySecurity(source, temp, before.state.access); err != nil {
		t.Fatal("probe copy", err)
	}
	actual, err := nativeMetadata(temp)
	if err != nil {
		t.Fatal("probe metadata", err)
	}
	t.Logf("stage=unprotected source-control=%x copied-control=%x owner=%t group=%t acl=%t attrs=%t source-length=%d copied-length=%d", before.state.access.control, actual.control, before.state.access.owner == actual.owner, before.state.access.group == actual.group, before.state.access.dacl == actual.dacl, before.state.access.attributes == actual.attributes, len(before.state.access.dacl), len(actual.dacl))
	if err := Replace(path, before, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if accessListing(t, path) != access {
		t.Fatal("unprotected inherited DACL changed")
	}
	digest, err := snapshot(t, path).AccessDigest()
	if err != nil || digest != candidates[0] {
		t.Fatal("unprotected digest prediction")
	}
}

func TestWindowsUnprotectedUnconvertedControlFailsClosed(t *testing.T) {
	path := filepath.Join(hostDirectory(t), "unconverted.json")
	writeFixtureDescriptor(t, path, []byte("old"), fixtureDescriptor(t, "D:", false))
	before := snapshot(t, path)
	if before.state.access.control&(windows.SE_DACL_PROTECTED|windows.SE_DACL_AUTO_INHERITED) != 0 {
		t.Fatal("unexpected initial fixture control")
	}
	access := accessListing(t, path)
	if _, err := before.ExpectedAccessDigests(true); err == nil {
		t.Fatal("unsupported unprotected control predicted")
	}
	if err := Replace(path, before, []byte("bad")); err == nil {
		t.Fatal("unprotected control transition accepted")
	}
	assertData(t, path, "old")
	if accessListing(t, path) != access {
		t.Fatal("unsupported source control changed")
	}
}

func TestWindowsUncopyableFileInheritanceFlagsFailClosed(t *testing.T) {
	path := filepath.Join(hostDirectory(t), "uncopyable.json")
	writeFixtureDescriptor(t, path, []byte("old"), fixtureDescriptor(t, "D:P", true))
	before := snapshot(t, path)
	access := accessListing(t, path)
	if _, err := before.ExpectedAccessDigests(true); err == nil {
		t.Fatal("uncopyable access predicted as supported")
	}
	if err := Replace(path, before, []byte("bad")); err == nil {
		t.Fatal("uncopyable ACL accepted")
	}
	assertData(t, path, "old")
	if accessListing(t, path) != access {
		t.Fatal("uncopyable source ACL changed")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed copy leaked temp")
	}
}

func TestWindowsDeniedWriteIsNotBypassed(t *testing.T) {
	path := filepath.Join(hostDirectory(t), "denied.json")
	writeFixture(t, path, []byte("old"))
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd := fixtureDescriptor(t, "D:P(D;;0x2;;;"+user.User.Sid.String()+")", false)
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, path)
	access := accessListing(t, path)
	if Replace(path, before, []byte("bad")) == nil || Remove(path, before) == nil {
		t.Fatal("DACL denial bypassed")
	}
	assertData(t, path, "old")
	if accessListing(t, path) != access {
		t.Fatal("DACL denial modified")
	}
}

func TestWindowsForeignOwnerPolicyAndNativeFixture(t *testing.T) {
	sd, err := windows.SecurityDescriptorFromString("O:BAD:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	if ownedDescriptor(sd) {
		t.Fatal("foreign owner descriptor accepted")
	}
	t.Run("native", func(t *testing.T) {
		path := filepath.Join(hostDirectory(t), "foreign")
		name, _ := windows.UTF16PtrFromString(path)
		sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
		h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		runtime.KeepAlive(sd)
		if err != nil {
			t.Skip("foreign-owner fixture unavailable without additional privileges")
		}
		windows.CloseHandle(h)
		actual, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		owner, _, err := actual.Owner()
		if err != nil || owner.String() != "S-1-5-32-544" {
			t.Fatal("foreign owner fixture not established")
		}
		if _, err := Read(path, 4096); err == nil {
			t.Fatal("native foreign owner accepted")
		}
	})
}

func assertCurrentOwner(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if owner == nil || !owner.Equals(user.User.Sid) {
		t.Fatal("fixture owner is not current user")
	}
}

func accessListing(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	// Access comparison permits only the reviewed protected-file AI marker;
	// dedicated tests below assert its direction and every other control bit.
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED != 0 {
		if err := sd.SetControl(windows.SE_DACL_AUTO_INHERITED, 0); err != nil {
			t.Fatal(err)
		}
	}
	name, _ := windows.UTF16PtrFromString(path)
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String() + string(rune(attrs & ^uint32(windows.FILE_ATTRIBUTE_ARCHIVE|windows.FILE_ATTRIBUTE_NORMAL)))
}

func changeAccess(t *testing.T, path string) {
	t.Helper()
	sd := fixtureDescriptor(t, "D:P(A;;FR;;;AU)", false)
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func assertOwnerOnly(t *testing.T, path string) {
	t.Helper()
	assertCurrentOwner(t, path)
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("new DACL unprotected")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatal("new DACL not owner-only")
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if windows.GetAce(acl, 0, &ace) != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask != 0x1f01ff {
		t.Fatal("wrong new-file ACE")
	}
	user, _ := windows.GetCurrentProcessToken().GetTokenUser()
	if !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user.User.Sid) {
		t.Fatal("new-file ACE not current user")
	}
}

func TestWindowsSharingReplacement(t *testing.T) {
	for _, sharing := range []bool{false, true} {
		path := fixture(t)
		before := snapshot(t, path)
		name, _ := windows.UTF16PtrFromString(path)
		share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
		if sharing {
			share |= windows.FILE_SHARE_DELETE
		}
		h, err := windows.CreateFile(name, windows.GENERIC_READ, share, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		reader := os.NewFile(uintptr(h), path)
		started := time.Now()
		err = Replace(path, before, []byte("whole new file"))
		elapsed := time.Since(started)
		old, readErr := io.ReadAll(reader)
		reader.Close()
		if readErr != nil || string(old) != `{"other":1}` {
			t.Fatal("old handle changed")
		}
		if sharing {
			if err != nil {
				t.Fatal(err)
			}
			assertData(t, path, "whole new file")
		} else {
			if err == nil {
				t.Fatal("nondelete-sharing reader bypassed")
			}
			if elapsed > 2*time.Second {
				t.Fatal("unbounded retry")
			}
			assertData(t, path, `{"other":1}`)
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 {
				t.Fatal("temp leaked")
			}
		}
	}
}

func TestWindowsReparseAndAncestorRejected(t *testing.T) {
	dir := hostDirectory(t)
	target := hostDirectory(t)
	path := filepath.Join(target, "host.json")
	writeFixture(t, path, []byte("untouched"))
	link := filepath.Join(dir, "junction")
	if err := os.Mkdir(link, 0700); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(link)
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	substitute, _ := windows.UTF16FromString(`\??\` + target)
	printed, _ := windows.UTF16FromString(target)
	data := make([]byte, 16+2*(len(substitute)+len(printed)))
	binary.LittleEndian.PutUint32(data, windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(data[4:], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:], uint16((len(substitute)-1)*2))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(data[14:], uint16((len(printed)-1)*2))
	for i, v := range append(substitute, printed...) {
		binary.LittleEndian.PutUint16(data[16+i*2:], v)
	}
	var returned uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &returned, nil)
	windows.CloseHandle(h)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, filepath.Join(link, "host.json")} {
		if _, err := Read(p, 4096); err == nil {
			t.Fatal("reparse accepted")
		}
	}
	assertData(t, path, "untouched")
}

func TestWindowsTemporarySecurityBeforeWrite(t *testing.T) {
	path := fixture(t)
	before := snapshot(t, path)
	source, err := nativeOpen(path, true)
	if err != nil {
		t.Fatal("stage=open", err)
	}
	defer source.Close()
	tempPath := filepath.Join(filepath.Dir(path), "new-empty-temp")
	temp, err := nativeCreate(tempPath)
	if err != nil {
		t.Fatal("stage=create", err)
	}
	defer temp.Close()
	assertOwnerOnly(t, tempPath)
	if err := nativeCopySecurity(source, temp, before.state.access); err != nil {
		t.Fatal("stage=copy", err)
	}
	actual, err := nativeMetadata(temp)
	if err != nil {
		t.Fatal("stage=metadata", err)
	}
	t.Logf("stage=security source-control=%x copied-control=%x descriptor-equal=%t attributes-equal=%t", before.state.access.control, actual.control, before.state.access.descriptor == actual.descriptor, before.state.access.attributes == actual.attributes)
	t.Logf("stage=identity owner-equal=%t group-equal=%t acl-equal=%t source-acl-length=%d copied-acl-length=%d", before.state.access.owner == actual.owner, before.state.access.group == actual.group, before.state.access.dacl == actual.dacl, len(before.state.access.dacl), len(actual.dacl))
	for i := range min(len(before.state.access.dacl), len(actual.dacl)) {
		if before.state.access.dacl[i] != actual.dacl[i] {
			t.Logf("stage=acl-difference offset=%d source=%x copied=%x", i, before.state.access.dacl[i], actual.dacl[i])
		}
	}
	if actual.owner != before.state.access.owner || actual.group != before.state.access.group || actual.dacl != before.state.access.dacl || actual.attributes != before.state.access.attributes || actual.control != before.state.access.control|windows.SE_DACL_AUTO_INHERITED {
		t.Fatal("temporary security not preserved before data")
	}
	info, err := temp.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatal("temporary not empty")
	}
}

func TestWindowsSecurityCopyAllowsOnlyProtectedForwardAI(t *testing.T) {
	base := nativeAccess{owner: "owner", group: "group", dacl: "exact ACE bytes", control: windows.SE_SELF_RELATIVE | windows.SE_DACL_PRESENT | windows.SE_DACL_PROTECTED}
	for _, kind := range []string{"same", "forward", "reverse", "unprotected", "owner", "group", "dacl", "other-control", "attributes"} {
		t.Run(kind, func(t *testing.T) {
			source, dest := base, base
			dest.control |= windows.SE_DACL_AUTO_INHERITED
			switch kind {
			case "same":
				dest = source
			case "reverse":
				source, dest = dest, source
			case "unprotected":
				dest.control &^= windows.SE_DACL_PROTECTED
			case "owner":
				dest.owner = "other"
			case "group":
				dest.group = "other"
			case "dacl":
				dest.dacl = "different ACE bytes"
			case "other-control":
				dest.control |= windows.SE_DACL_DEFAULTED
			case "attributes":
				dest.attributes = windows.FILE_ATTRIBUTE_READONLY
			}
			want := kind == "same" || kind == "forward"
			if preservedAccess(source, dest) != want {
				t.Fatal("unsafe security-copy equivalence")
			}
		})
	}
}

func TestWindowsDeviceAndAlternateNamesRejected(t *testing.T) {
	for _, name := range []string{`C:\host\NUL`, `C:\host\COM1.txt`, `C:\host\file:stream`, `C:\host\file.`, `\\?\C:\host\file`} {
		if nativePath(name) {
			t.Fatal("ambiguous or device path accepted")
		}
	}
}
