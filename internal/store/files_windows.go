package store

import (
	"errors"
	"os"
	"runtime"
	"unsafe"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/winfile"
	"golang.org/x/sys/windows"
)

func nativeErrorCategory(err error) string {
	switch {
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return "sharing"
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return "access"
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND), errors.Is(err, windows.ERROR_PATH_NOT_FOUND):
		return "missing"
	case errors.Is(err, windows.ERROR_FILE_EXISTS), errors.Is(err, windows.ERROR_ALREADY_EXISTS):
		return "exists"
	case errors.Is(err, windows.ERROR_DISK_FULL), errors.Is(err, windows.ERROR_WRITE_FAULT), errors.Is(err, windows.ERROR_READ_FAULT):
		return "io"
	default:
		return "other"
	}
}

func privateDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)")
}

func nativeMkdir(path string) error {
	sd, err := privateDescriptor()
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	err = windows.CreateDirectory(name, &sa)
	runtime.KeepAlive(sd)
	return err
}

func nativeReparse(path string) bool {
	return nativeReparseFailure(path) != nil
}

func nativeReparseFailure(path string) *privateStateError {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return &privateStateError{"reparse-path", "rejected"}
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		return &privateStateError{"reparse-query", nativeErrorCategory(err)}
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return &privateStateError{"reparse-point", "rejected"}
	}
	return nil
}

func descriptorPrivate(sd *windows.SECURITY_DESCRIPTOR) bool {
	if sd == nil {
		return false
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 2 {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.IsValid() || !owner.Equals(user.User.Sid) {
		return false
	}
	foundUser, foundSystem := false, false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil {
			return false
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Mask != 0x1f01ff {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch sid.String() {
		case user.User.Sid.String():
			foundUser = true
		case "S-1-5-18":
			foundSystem = true
		default:
			return false
		}
	}
	return foundUser && foundSystem
}

func nativePrivate(path string, directory bool) bool {
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) || nativeReparse(path) {
		return false
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	return err == nil && descriptorPrivate(sd)
}

// System-owned ancestors are trusted, never accepted as private state leaves.
func nativeTrustedAncestor(path string) bool {
	return nativeTrustedAncestorFailure(path) == nil
}

func nativeTrustedAncestorFailure(path string) *privateStateError {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return &privateStateError{"owner-query", nativeErrorCategory(err)}
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return &privateStateError{"owner-value", nativeErrorCategory(err)}
	}
	if owner == nil || !owner.IsValid() {
		return &privateStateError{"owner-value", "rejected"}
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return &privateStateError{"current-user-query", nativeErrorCategory(err)}
	}
	if user.User.Sid == nil || !user.User.Sid.IsValid() {
		return &privateStateError{"current-user-value", "rejected"}
	}
	if trustedAncestorOwner(owner, user.User.Sid, nil) {
		return nil
	}
	// Resolve only this local OS service, not the All Services group or an
	// account name derived from input. Resolution failure never grants access.
	installer, _, _, err := windows.LookupSID("", `NT SERVICE\TrustedInstaller`)
	if err != nil {
		return &privateStateError{"installer-lookup", nativeErrorCategory(err)}
	}
	if !trustedAncestorOwner(owner, user.User.Sid, installer) {
		return &privateStateError{"ancestor-owner", "rejected"}
	}
	return nil
}

// installer is nil or the SID resolved for the fixed local TrustedInstaller
// principal. This allowance is ancestor-only; descriptorPrivate stays strict.
func trustedAncestorOwner(owner, current, installer *windows.SID) bool {
	if owner == nil || current == nil || !owner.IsValid() || !current.IsValid() {
		return false
	}
	return owner.Equals(current) || owner.String() == "S-1-5-18" || owner.String() == "S-1-5-32-544" ||
		(installer != nil && installer.IsValid() && owner.Equals(installer))
}

func nativeOpen(path string, exclusive bool) (*os.File, error) {
	sd, err := privateDescriptor()
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	created := err == nil
	if !exclusive && (errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS)) {
		h, err = windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	}
	runtime.KeepAlive(sd)
	if err != nil {
		return nil, err
	}
	return finishNativeOpen(path, h, created)
}

func finishNativeOpen(path string, h windows.Handle, created bool) (*os.File, error) {
	var info windows.ByHandleFileInformation
	actual, aclErr := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || aclErr != nil || !descriptorPrivate(actual) {
		windows.CloseHandle(h)
		if created {
			os.Remove(path)
		}
		return nil, errPrivate
	}
	return os.NewFile(uintptr(h), path), nil
}

func nativeReplace(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	// Open the already-synced temporary file without following a reparse point.
	h, err := windows.CreateFile(source, windows.DELETE|windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	sd, aclErr := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || aclErr != nil || !descriptorPrivate(sd) {
		return errPrivate
	}
	return winfile.Replace(h, to)
}
