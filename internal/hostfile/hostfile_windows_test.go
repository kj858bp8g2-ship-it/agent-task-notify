package hostfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		t.Fatal("stage=unprotected-prediction", predictionDetails(before))
	}
	assertPredictedTemporary(t, path, before, candidates)
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
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.LABEL_SECURITY_INFORMATION|windows.ATTRIBUTE_SECURITY_INFORMATION|windows.SCOPE_SECURITY_INFORMATION)
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
	temp, err := nativeCreate(tempPath, false)
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
	for _, kind := range []string{"same", "forward", "reverse", "unprotected", "owner", "group", "dacl", "other-control", "attributes", "policy", "policy-presence", "sacl-control"} {
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
			case "policy":
				dest.policy = "different label"
			case "policy-presence":
				dest.policyPresent = true
			case "sacl-control":
				dest.control |= windows.SE_SACL_AUTO_INHERITED
			}
			want := kind == "same" || kind == "forward"
			if preservedAccess(source, dest) != want {
				t.Fatal("unsafe security-copy equivalence")
			}
		})
	}
}

func TestWindowsDeviceAndAlternateNamesRejected(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		allowed    bool
	}{
		{"bare-device", `C:\host\NUL`, false},
		{"device-extension", `C:\host\COM1.txt`, false},
		{"device-spaced-extension", `C:\host\lPt9 .json`, false},
		{"device-multiple-extensions", `C:\host\aux.settings.json`, false},
		{"alternate-stream", `C:\host\file:stream`, false},
		{"trailing-dot", `C:\host\file.`, false},
		{"device-namespace", `\\?\C:\host\file`, false},
		{"dotfile", `C:\host\.settings.json`, true},
		{"ordinary-extension", `C:\host\settings.json`, true},
		{"device-prefix-only", `C:\host\COM10.json`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if nativePath(tc.path) != tc.allowed {
				t.Fatal("path category policy mismatch")
			}
		})
	}
}

// Fixed bounded fields only: never print paths, SID strings or ACL bytes.
func predictionDetails(s Snapshot) string {
	if s.state == nil {
		return "snapshot=invalid"
	}
	a := s.state.access
	if !s.state.exists {
		a = s.state.newAccess
	}
	count, kind, flags := -1, -1, -1
	if len(a.policy) >= 8 {
		count = int(binary.LittleEndian.Uint16([]byte(a.policy[4:6])))
	}
	if count > 0 && len(a.policy) >= 12 {
		kind, flags = int(a.policy[8]), int(a.policy[9])
	}
	const propagation = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE | windows.INHERIT_ONLY_ACE | windows.NO_PROPAGATE_INHERIT_ACE
	const saclControls = windows.SE_SACL_PRESENT | windows.SE_SACL_DEFAULTED | windows.SE_SACL_AUTO_INHERIT_REQ | windows.SE_SACL_AUTO_INHERITED | windows.SE_SACL_PROTECTED
	malformed, propagatingDACL := len(a.dacl) < 8, false
	if !malformed {
		offset := 8
		for range int(binary.LittleEndian.Uint16([]byte(a.dacl[4:6]))) {
			if offset+4 > len(a.dacl) {
				malformed = true
				break
			}
			size := int(binary.LittleEndian.Uint16([]byte(a.dacl[offset+2 : offset+4])))
			if size < 4 || offset+size > len(a.dacl) {
				malformed = true
				break
			}
			propagatingDACL = propagatingDACL || a.dacl[offset+1]&propagation != 0
			offset += size
		}
	}
	return fmt.Sprintf("control=%04x attrs=%x policy-present=%t policy-length=%d policy-count=%d policy-type=%d policy-flags=%d readonly=%t residual-policy=%t propagating-policy=%t unconverted-dacl=%t malformed-dacl=%t propagating-dacl=%t",
		a.control, a.attributes, a.policyPresent, len(a.policy), count, kind, flags, !a.writable(), len(a.policy) <= 8 && (a.policy != "" || a.policyPresent || a.control&(saclControls & ^windows.SE_SACL_AUTO_INHERITED) != 0), flags >= 0 && flags&propagation != 0, a.control&(windows.SE_DACL_PROTECTED|windows.SE_DACL_AUTO_INHERITED) == 0, malformed, propagatingDACL)
}

func TestWindowsPredictionDiagnosticDoesNotExposeMetadata(t *testing.T) {
	canary := "synthetic-sensitive-value"
	s := Snapshot{Data: []byte(canary), state: &observation{exists: true, path: canary, access: nativeAccess{owner: canary, group: canary, dacl: canary, policy: canary, descriptor: canary}}}
	got := predictionDetails(s)
	if len(got) > 384 || strings.Contains(got, canary) {
		t.Fatal("prediction diagnostic leaked or exceeded bound")
	}
	if predictionDetails(Snapshot{}) != "snapshot=invalid" {
		t.Fatal("invalid snapshot diagnostic")
	}
}

// All policy mutations in these tests apply only to newly created, explicitly
// owned synthetic fixtures. No privilege is enabled and no full SACL is read.
func setFixturePolicy(t *testing.T, path, policy string, field windows.SECURITY_INFORMATION) error {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(policy)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.SACL()
	if err != nil || acl == nil {
		t.Fatal("invalid synthetic access policy", err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, field, nil, nil, nil, acl)
	runtime.KeepAlive(sd)
	return err
}

func fixturePolicy(t *testing.T, path string) (string, windows.SECURITY_DESCRIPTOR_CONTROL) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION|windows.ATTRIBUTE_SECURITY_INFORMATION|windows.SCOPE_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	return sd.String(), control
}

func TestWindowsLowLabelPreservedBeforeWriteAndAfterReplace(t *testing.T) {
	path := fixture(t)
	if err := setFixturePolicy(t, path, "S:(ML;;NW;;;LW)", windows.LABEL_SECURITY_INFORMATION); err != nil {
		t.Fatal(err)
	}
	policy, control := fixturePolicy(t, path)
	if policy != "S:AI(ML;;NW;;;LW)" || control&windows.SE_SACL_PRESENT == 0 {
		t.Fatal("Low/NW fixture was not established", policy)
	}
	before := snapshot(t, path)
	want, err := before.ExpectedAccessDigests(true)
	if err != nil || len(want) > 2 {
		t.Fatal("label prediction", err)
	}
	source, err := nativeOpen(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	temp, _, err := prepareTemporary(path, source, before.state.access)
	if err != nil {
		t.Fatal("prepare labeled empty temporary", err)
	}
	identity, _ := temp.Stat()
	defer cleanupTemporary(temp.Name(), identity)
	defer temp.Close()
	got, gotControl := fixturePolicy(t, temp.Name())
	if identity.Size() != 0 || got != policy || gotControl&(windows.SE_SACL_PRESENT|windows.SE_SACL_AUTO_INHERITED|windows.SE_SACL_PROTECTED) != control&(windows.SE_SACL_PRESENT|windows.SE_SACL_AUTO_INHERITED|windows.SE_SACL_PROTECTED) {
		t.Fatal("Low/NW policy missing before the first payload byte", got)
	}
	if err := Replace(path, before, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ = fixturePolicy(t, path)
	if got != policy {
		t.Fatal("Low/NW policy changed after replace")
	}
	after := snapshot(t, path)
	digest, _ := after.AccessDigest()
	if digest != want[0] && (len(want) != 2 || digest != want[1]) {
		t.Fatal("labeled access prediction mismatch")
	}
	if err := Remove(path, after); err != nil {
		t.Fatal(err)
	}
	t.Log("native Low/NW preserved before payload, after replace and through digest; labeled remove succeeded")
}

func TestWindowsLabelOnlyChangesConflictDigestAndPostverify(t *testing.T) {
	path := fixture(t)
	before := snapshot(t, path)
	digest, _ := before.AccessDigest()
	if err := setFixturePolicy(t, path, "S:(ML;;NW;;;LW)", windows.LABEL_SECURITY_INFORMATION); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, path)
	changed, _ := after.AccessDigest()
	if digest == changed {
		t.Fatal("label-only change omitted from digest")
	}
	policy, _ := fixturePolicy(t, path)
	if Replace(path, before, []byte("bad")) == nil || Remove(path, before) == nil {
		t.Fatal("label-only conflict accepted")
	}
	assertData(t, path, string(before.Data))
	if got, _ := fixturePolicy(t, path); got != policy {
		t.Fatal("conflict changed source label")
	}
	if err := verifyReplacement(path, before.Data, before.state.access, before.state.file); !errors.Is(err, ErrVerification) {
		t.Fatal("label-only postverification did not fail closed", err)
	}
}

func TestWindowsNewTargetExplicitLabelPrediction(t *testing.T) {
	path := filepath.Join(hostDirectory(t), "new.json")
	before := snapshot(t, path)
	want, err := before.ExpectedAccessDigests(true)
	if err != nil || len(want) != 1 {
		t.Fatal("new access must have exactly one prediction", err)
	}
	temp, actual, err := prepareTemporary(path, nil, nativeAccess{})
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := temp.Stat()
	defer cleanupTemporary(temp.Name(), identity)
	defer temp.Close()
	policy, control := fixturePolicy(t, temp.Name())
	if identity.Size() != 0 || policy == "" || control&windows.SE_SACL_PRESENT == 0 {
		t.Fatal("new explicit integrity policy missing before payload")
	}
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	// Use native-word alignment for the independently queried pointer-bearing
	// TOKEN_MANDATORY_LABEL (a fixed byte array can be stack-byte-aligned).
	buffer := make([]uintptr, 512)
	var returned uint32
	if err := windows.GetTokenInformation(token, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&buffer[0])), uint32(len(buffer))*uint32(unsafe.Sizeof(buffer[0])), &returned); err != nil {
		t.Fatal(err)
	}
	label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buffer[0])).Label
	sd, err := windows.GetNamedSecurityInfo(temp.Name(), windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.SACL()
	var ace *windows.ACCESS_ALLOWED_ACE
	if err != nil || acl == nil || acl.AceCount != 1 || windows.GetAce(acl, 0, &ace) != nil || ace.Header.AceType != 17 || ace.Header.AceFlags != 0 || ace.Mask != 1 || !(*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(label.Sid) {
		t.Fatal("new policy is not current token integrity / NW / flags0")
	}
	runtime.KeepAlive(buffer)
	full, err := windows.GetNamedSecurityInfo(temp.Name(), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.LABEL_SECURITY_INFORMATION|windows.ATTRIBUTE_SECURITY_INFORMATION|windows.SCOPE_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	fullControl, _, err := full.Control()
	if err != nil || fullControl != windows.SE_SELF_RELATIVE|windows.SE_DACL_PRESENT|windows.SE_DACL_PROTECTED|windows.SE_SACL_PRESENT|windows.SE_SACL_AUTO_INHERITED {
		t.Fatal("new policy did not have precisely predicted control bits")
	}
	if actual != before.state.newAccess || accessDigest(true, actual) != want[0] {
		t.Fatal("new target precise pre-write prediction mismatch")
	}
	if err := Replace(path, before, []byte("new")); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, path)
	got, _ := after.AccessDigest()
	if got != want[0] {
		t.Fatal("new target precise postverify prediction mismatch")
	}
	if gotPolicy, gotControl := fixturePolicy(t, path); gotPolicy != policy || gotControl != control {
		t.Fatal("new target label/control changed")
	}
	assertOwnerOnly(t, path)
}

func TestWindowsComplexPolicyRejected(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		field        windows.SECURITY_INFORMATION
		typeID       byte
	}{
		{"attribute", `S:(RA;;;;;WD;("Synthetic",TS,0,"value"))`, windows.ATTRIBUTE_SECURITY_INFORMATION, 18},
		{"scope", "S:(SP;;;;;S-1-17-1)", windows.SCOPE_SECURITY_INFORMATION, 19},
	} {
		t.Run(tc.name+"-parser", func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(fixtureDescriptor(t, "D:P", false).String() + tc.policy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := accessFromDescriptor(sd, 0); !errors.Is(err, ErrUnsafe) {
				t.Fatal("unsupported access ACE accepted", err)
			}
		})
		t.Run(tc.name+"-native", func(t *testing.T) {
			path := fixture(t)
			before := snapshot(t, path)
			if err := setFixturePolicy(t, path, tc.policy, tc.field); err != nil {
				if tc.name == "scope" && (errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD)) {
					t.Skip("SCOPE fixture unavailable with existing rights; native rejection unverified; pure ACE rejection is separate")
				}
				t.Fatal(err)
			}
			sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, tc.field)
			if err != nil {
				t.Fatal(err)
			}
			acl, _, err := sd.SACL()
			var ace *windows.ACCESS_ALLOWED_ACE
			if err != nil || acl == nil || acl.AceCount != 1 || windows.GetAce(acl, 0, &ace) != nil || ace.Header.AceType != tc.typeID {
				t.Fatal("native access-policy fixture not established")
			}
			policy, _ := fixturePolicy(t, path)
			if _, err := Read(path, 4096); !errors.Is(err, ErrUnsafe) {
				t.Fatal("complex policy read accepted", err)
			}
			if Replace(path, before, []byte("bad")) == nil || Remove(path, before) == nil {
				t.Fatal("complex policy mutation accepted")
			}
			assertData(t, path, string(before.Data))
			if got, _ := fixturePolicy(t, path); got != policy {
				t.Fatal("source policy changed")
			}
		})
	}
}

func TestWindowsParentAccessPolicyRefusedWithoutWrites(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		field        windows.SECURITY_INFORMATION
	}{
		{"inheritable-label", "S:(ML;OICI;NW;;;LW)", windows.LABEL_SECURITY_INFORMATION},
		{"attribute", `S:(RA;;;;;WD;("Synthetic",TS,0,"value"))`, windows.ATTRIBUTE_SECURITY_INFORMATION},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := hostDirectory(t)
			path := filepath.Join(dir, "new.json")
			before := snapshot(t, path)
			if err := setFixturePolicy(t, dir, tc.policy, tc.field); err != nil {
				t.Fatal(err)
			}
			policy, _ := fixturePolicy(t, dir)
			if tc.name == "inheritable-label" {
				sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
				if err != nil {
					t.Fatal(err)
				}
				acl, _, err := sd.SACL()
				var ace *windows.ACCESS_ALLOWED_ACE
				if err != nil || acl == nil || acl.AceCount != 1 || windows.GetAce(acl, 0, &ace) != nil || ace.Header.AceType != 17 || ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE {
					t.Fatal("native inheritable parent label was not established")
				}
			}
			if _, err := Read(path, 4096); !errors.Is(err, ErrUnsafe) {
				t.Fatal("parent access policy accepted", err)
			}
			if err := Replace(path, before, []byte("bad")); err == nil {
				t.Fatal("parent policy change accepted")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal("parent policy refusal wrote a file")
			}
			if got, _ := fixturePolicy(t, dir); got != policy {
				t.Fatal("parent policy changed")
			}
		})
	}
}

func TestWindowsTokenIntegrityValidation(t *testing.T) {
	for _, tc := range []struct {
		sid        string
		attributes uint32
		valid      bool
	}{
		{"S-1-16-4096", windows.SE_GROUP_INTEGRITY, true},
		{"S-1-16-8192", windows.SE_GROUP_INTEGRITY | windows.SE_GROUP_INTEGRITY_ENABLED, true},
		{"S-1-16-8448", windows.SE_GROUP_INTEGRITY, true},
		{"S-1-16-12288", windows.SE_GROUP_INTEGRITY, true},
		{"S-1-16-0", windows.SE_GROUP_INTEGRITY, false},
		{"S-1-16-16384", windows.SE_GROUP_INTEGRITY, false},
		{"S-1-16-20480", windows.SE_GROUP_INTEGRITY, false},
		{"S-1-16-8193", windows.SE_GROUP_INTEGRITY, false},
		{"S-1-5-8192", windows.SE_GROUP_INTEGRITY, false},
		{"S-1-16-8192-1", windows.SE_GROUP_INTEGRITY, false},
		{"S-1-16-8192", 0, false},
		{"S-1-16-8192", windows.SE_GROUP_INTEGRITY_ENABLED, false},
		{"S-1-16-8192", windows.SE_GROUP_INTEGRITY | windows.SE_GROUP_ENABLED, false},
	} {
		sid, err := windows.StringToSid(tc.sid)
		if err != nil {
			t.Fatal(err)
		}
		if validTokenIntegrity(windows.SIDAndAttributes{Sid: sid, Attributes: tc.attributes}) != tc.valid {
			t.Fatal("wrong token integrity validation")
		}
	}
	if validTokenIntegrity(windows.SIDAndAttributes{Attributes: windows.SE_GROUP_INTEGRITY}) {
		t.Fatal("nil token label accepted")
	}
}

func TestWindowsPolicyParserAndCanonicalBoundaries(t *testing.T) {
	base := fixtureDescriptor(t, "D:P", false).String()
	for _, kind := range []string{"repeated", "wrong-authority", "unknown", "invalid-mask", "invalid-flags"} {
		sd, err := windows.SecurityDescriptorFromString(base + "S:(ML;;NW;;;LW)")
		if err != nil {
			t.Fatal(err)
		}
		acl, _, err := sd.SACL()
		if err != nil {
			t.Fatal(err)
		}
		// Pure descriptor counterexamples: mutate only this allocated synthetic
		// descriptor, never a filesystem ACL. Windows rejects invalid ML SDDL.
		data := unsafe.Slice((*byte)(unsafe.Pointer(acl)), 28)
		switch kind {
		case "repeated":
			data[4] = 2
		case "wrong-authority":
			data[23] = 5
		case "unknown":
			data[8] = 42
		case "invalid-mask":
			data[12] = 8
		case "invalid-flags":
			data[9] = 128
		}
		if _, err := accessFromDescriptor(sd, 0); !errors.Is(err, ErrUnsafe) {
			t.Fatal("unknown, repeated or invalid label accepted", kind)
		}
	}
	sd, err := windows.SecurityDescriptorFromString(base + "S:(ML;;NW;;;LW)")
	if err != nil {
		t.Fatal(err)
	}
	access, err := accessFromDescriptor(sd, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"policy", "presence", "control"} {
		changed := access
		switch mutation {
		case "policy":
			changed.policy = changed.policy[:len(changed.policy)-1] + "x"
		case "presence":
			changed.policyPresent = !changed.policyPresent
		case "control":
			changed.control ^= windows.SE_SACL_AUTO_INHERITED
		}
		if accessDigest(true, changed) == accessDigest(true, access) {
			t.Fatal("policy omitted from canonical access")
		}
		if preservedAccess(access, changed) {
			t.Fatal("policy change accepted as R44")
		}
	}
	// Query failure is not interpreted as policy absence.
	file := os.NewFile(uintptr(windows.InvalidHandle), "invalid-synthetic-handle")
	if _, err := nativeMetadata(file); !errors.Is(err, ErrUnsafe) {
		t.Fatal("invalid handle metadata accepted")
	}
}

func TestWindowsAbsentLabelSourceDoesNotAcquirePolicy(t *testing.T) {
	path := fixture(t)
	before := snapshot(t, path)
	policy, control := fixturePolicy(t, path)
	if policy != "" || control&windows.SE_SACL_PRESENT != 0 {
		t.Fatal("fixture has unexpected access policy")
	}
	if err := Replace(path, before, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, actual := fixturePolicy(t, path)
	if got != policy || actual & ^windows.SECURITY_DESCRIPTOR_CONTROL(windows.SE_DACL_AUTO_INHERITED) != control {
		t.Fatal("unlabeled source gained policy or SACL controls")
	}
}

func TestWindowsNoninheritableParentLabelDoesNotDefineNewPolicy(t *testing.T) {
	dir := hostDirectory(t)
	if err := setFixturePolicy(t, dir, "S:(ML;;NW;;;LW)", windows.LABEL_SECURITY_INFORMATION); err != nil {
		t.Fatal(err)
	}
	parent, _ := fixturePolicy(t, dir)
	path := filepath.Join(dir, "new.json")
	before := snapshot(t, path)
	want, err := before.ExpectedAccessDigests(true)
	if err != nil || len(want) != 1 {
		t.Fatal(err)
	}
	if err := Replace(path, before, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ := snapshot(t, path).AccessDigest()
	if got != want[0] {
		t.Fatal("explicit new policy prediction changed under labeled parent")
	}
	if actual, _ := fixturePolicy(t, dir); actual != parent {
		t.Fatal("parent was modified")
	}
}

func TestWindowsPropagatingFileLabelNotPredicted(t *testing.T) {
	base := fixtureDescriptor(t, "D:P", false).String()
	sd, err := windows.SecurityDescriptorFromString(base + "S:(ML;OICI;NW;;;LW)")
	if err != nil {
		t.Fatal(err)
	}
	access, err := accessFromDescriptor(sd, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nativeExpectedAccess(access, true); !errors.Is(err, ErrUnsafe) {
		t.Fatal("propagating file label predicted despite possible ACE rewrite")
	}
}

func TestWindowsClearedLabelControlIsNotR44(t *testing.T) {
	path := fixture(t)
	if err := setFixturePolicy(t, path, "S:(ML;;NW;;;LW)", windows.LABEL_SECURITY_INFORMATION); err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, path)
	policy, control := fixturePolicy(t, path)
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.SACL()
	if err != nil || acl != nil || control&windows.SE_SACL_AUTO_INHERITED == 0 {
		t.Fatal("cleared-label residual control fixture not established")
	}
	if _, err := before.ExpectedAccessDigests(true); !errors.Is(err, ErrUnsafe) {
		t.Fatal("uncopyable residual SACL controls predicted")
	}
	if err := Replace(path, before, []byte("bad")); !errors.Is(err, ErrUnsafe) {
		t.Fatal("cleared SACL controls silently normalized", err)
	}
	assertData(t, path, string(before.Data))
	if actual, actualControl := fixturePolicy(t, path); actual != policy || actualControl != control {
		t.Fatal("source residual policy changed")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("failed copy leaked temporary")
	}
}

func TestWindowsAbsentPolicyPredictionRetainsOnlyExistingSACLAI(t *testing.T) {
	bits := []windows.SECURITY_DESCRIPTOR_CONTROL{windows.SE_SACL_PRESENT, windows.SE_SACL_DEFAULTED, windows.SE_SACL_AUTO_INHERIT_REQ, windows.SE_SACL_AUTO_INHERITED, windows.SE_SACL_PROTECTED}
	for _, dacl := range []string{"D:P", "D:AI"} {
		base, err := accessFromDescriptor(fixtureDescriptor(t, dacl, false), 0)
		if err != nil {
			t.Fatal("fixture descriptor invalid")
		}
		for mask := 0; mask < 1<<len(bits); mask++ {
			var controls windows.SECURITY_DESCRIPTOR_CONTROL
			for i, bit := range bits {
				if mask&(1<<i) != 0 {
					controls |= bit
				}
			}
			for _, form := range []string{"absent", "null", "empty"} {
				t.Run(fmt.Sprintf("%s/%x/%s", dacl, controls, form), func(t *testing.T) {
					a := base
					a.control |= controls
					a.policyPresent = form != "absent"
					if form == "empty" {
						a.policy = "\x02\x00\x08\x00\x00\x00\x00\x00"
					}
					want := form == "absent" && (controls == 0 || controls == windows.SE_SACL_AUTO_INHERITED)
					got, err := nativeExpectedAccess(a, true)
					if !want {
						if !errors.Is(err, ErrUnsafe) || len(got) != 0 {
							t.Fatal("unsupported policy controls predicted")
						}
						return
					}
					count := 1
					if dacl == "D:P" {
						count = 2
					}
					if err != nil || len(got) != count {
						t.Fatal("unchanged absent policy controls not predicted")
					}
					for _, candidate := range got {
						if candidate.control&windows.SE_SACL_AUTO_INHERITED != controls&windows.SE_SACL_AUTO_INHERITED || !preservedAccess(a, candidate) {
							t.Fatal("candidate changed SACL controls or widened R44")
						}
					}
					changed := a
					changed.control ^= windows.SE_SACL_AUTO_INHERITED
					if preservedAccess(a, changed) || preservedAccess(changed, a) || accessDigest(true, a) == accessDigest(true, changed) {
						t.Fatal("SACL AI change ignored")
					}
				})
			}
		}
	}
}
