package hostfile

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unsafe"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/winfile"
	"golang.org/x/sys/windows"
)

const copySecurityFields = windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION

// These access-policy portions require READ_CONTROL, not full SACL access.
const securityFields = copySecurityFields | windows.LABEL_SECURITY_INFORMATION | windows.ATTRIBUTE_SECURITY_INFORMATION | windows.SCOPE_SECURITY_INFORMATION

type nativeAccess struct {
	descriptor         string
	owner, group, dacl string
	control            windows.SECURITY_DESCRIPTOR_CONTROL
	attributes         uint32
	policyPresent      bool
	policy             string
}

func (access nativeAccess) canonical() []byte {
	return canonicalFields("windows-access-v2", access.owner, access.group, access.dacl, strconv.FormatUint(uint64(access.control), 10), strconv.FormatUint(uint64(access.attributes), 10), strconv.FormatBool(access.policyPresent), access.policy)
}

func nativeDefaultAccess(parent string) (nativeAccess, error) {
	if _, err := nativeParent(parent); err != nil {
		return nativeAccess{}, ErrUnsafe
	}
	sd, err := creationDescriptor(true)
	if err != nil {
		return nativeAccess{}, ErrUnsafe
	}
	// Deliberate SetSecurityInfo(LABEL) initialization marks the SACL as AI.
	// This is a new explicit creation policy, not Windows' implicit default.
	// A different OS result is rejected before the first payload byte.
	if err := sd.SetControl(windows.SE_SACL_AUTO_INHERITED, windows.SE_SACL_AUTO_INHERITED); err != nil {
		return nativeAccess{}, ErrUnsafe
	}
	return accessFromDescriptor(sd, 0)
}

func validTokenIntegrity(label windows.SIDAndAttributes) bool {
	if label.Sid == nil || !label.Sid.IsValid() || label.Sid.IdentifierAuthority() != windows.SECURITY_MANDATORY_LABEL_AUTHORITY || label.Sid.SubAuthorityCount() != 1 ||
		label.Attributes&windows.SE_GROUP_INTEGRITY == 0 || label.Attributes & ^uint32(windows.SE_GROUP_INTEGRITY|windows.SE_GROUP_INTEGRITY_ENABLED) != 0 {
		return false
	}
	switch label.Sid.SubAuthority(0) {
	case 0x1000, 0x2000, 0x2100, 0x3000: // LOW, MEDIUM, MEDIUM_PLUS, HIGH only.
		return true
	}
	return false
}

func creationDescriptor(newTarget bool) (*windows.SECURITY_DESCRIPTOR, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return nil, ErrUnsafe
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil || user.User.Sid == nil || !user.User.Sid.IsValid() {
		return nil, ErrUnsafe
	}
	group, err := token.GetTokenPrimaryGroup()
	if err != nil || group.PrimaryGroup == nil || !group.PrimaryGroup.IsValid() {
		return nil, ErrUnsafe
	}
	value := "O:" + user.User.Sid.String() + "G:" + group.PrimaryGroup.String() + "D:P(A;;FA;;;" + user.User.Sid.String() + ")"
	if newTarget {
		var size uint32
		if err := windows.GetTokenInformation(token, windows.TokenIntegrityLevel, nil, 0, &size); err != windows.ERROR_INSUFFICIENT_BUFFER || size < uint32(unsafe.Sizeof(windows.Tokenmandatorylabel{})) || size > 4096 {
			return nil, ErrUnsafe
		}
		buffer := make([]byte, size)
		if err := windows.GetTokenInformation(token, windows.TokenIntegrityLevel, &buffer[0], size, &size); err != nil {
			return nil, ErrUnsafe
		}
		label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buffer[0])).Label
		if !validTokenIntegrity(label) {
			return nil, ErrUnsafe
		}
		// HIGH can be stricter than an implicit medium label. Do not lower it.
		value += "S:(ML;;NW;;;" + label.Sid.String() + ")"
		runtime.KeepAlive(buffer)
	}
	sd, err := windows.SecurityDescriptorFromString(value)
	if err != nil {
		return nil, ErrUnsafe
	}
	return sd, nil
}

func nativeExpectedAccess(access nativeAccess, existing bool) ([]nativeAccess, error) {
	if !access.writable() {
		return nil, ErrUnsafe
	}
	if existing {
		const saclControls = windows.SE_SACL_PRESENT | windows.SE_SACL_DEFAULTED | windows.SE_SACL_AUTO_INHERIT_REQ | windows.SE_SACL_AUTO_INHERITED | windows.SE_SACL_PROTECTED
		if (len(access.policy) <= 8 && (access.policyPresent || access.control&saclControls != 0)) ||
			(len(access.policy) > 8 && access.policy[9]&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE|windows.INHERIT_ONLY_ACE|windows.NO_PROPAGATE_INHERIT_ACE) != 0) {
			// No label copy can reproduce residual SACL controls, and regular
			// file propagation flags may be rewritten. Do not predict either.
			return nil, ErrUnsafe
		}
		if access.control&(windows.SE_DACL_PROTECTED|windows.SE_DACL_AUTO_INHERITED) == 0 {
			return nil, ErrUnsafe
		}
		// SetSecurityInfo removes propagation flags from ordinary file ACEs;
		// that is an ACL change, not the single allowed protected AI transition.
		acl := []byte(access.dacl)
		if len(acl) < 8 {
			return nil, ErrUnsafe
		}
		count := int(acl[4]) | int(acl[5])<<8
		offset := 8
		for range count {
			if offset+4 > len(acl) {
				return nil, ErrUnsafe
			}
			size := int(acl[offset+2]) | int(acl[offset+3])<<8
			if size < 4 || offset+size > len(acl) || acl[offset+1]&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE|windows.INHERIT_ONLY_ACE|windows.NO_PROPAGATE_INHERIT_ACE) != 0 {
				return nil, ErrUnsafe
			}
			offset += size
		}
	}
	result := []nativeAccess{access}
	if existing && access.control&windows.SE_DACL_PROTECTED != 0 && access.control&windows.SE_DACL_AUTO_INHERITED == 0 {
		copy := access
		copy.control |= windows.SE_DACL_AUTO_INHERITED
		result = append(result, copy)
	}
	return result, nil
}

func preservedAccess(source, destination nativeAccess) bool {
	if source == destination {
		return true
	}
	// SetSecurityInfo can mark a protected regular file as auto-inherited. This
	// single forward transition is accepted only with identical owner/group,
	// complete ACL bytes, attributes and every other descriptor control bit.
	return source.control&windows.SE_DACL_PROTECTED != 0 && source.control&windows.SE_DACL_AUTO_INHERITED == 0 &&
		destination.control == source.control|windows.SE_DACL_AUTO_INHERITED && source.owner == destination.owner &&
		source.group == destination.group && source.dacl == destination.dacl && source.attributes == destination.attributes &&
		source.policyPresent == destination.policyPresent && source.policy == destination.policy
}

func (access nativeAccess) writable() bool {
	return access.attributes&windows.FILE_ATTRIBUTE_READONLY == 0
}

func nativePath(path string) bool {
	// Device namespaces, alternate streams and ambiguous trailing dots/spaces
	// are not ordinary host configuration names.
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' {
		return false
	}
	for _, part := range strings.Split(path[len(volume):], string(filepath.Separator)) {
		if (part != "" && !filepath.IsLocal(part)) || strings.Contains(part, ":") || strings.TrimRight(part, ". ") != part {
			return false
		}
	}
	return true
}

func nativeUnlinked(path string) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(name)
	return err == nil && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0
}

func ownedDescriptor(sd *windows.SECURITY_DESCRIPTOR) bool {
	if sd == nil || !sd.IsValid() {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	return err == nil && owner.Equals(user.User.Sid)
}

func nativeParent(path string) (os.FileInfo, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, ErrUnsafe
	}
	h, err := windows.CreateFile(name, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, ErrUnsafe
	}
	file := os.NewFile(uintptr(h), path)
	defer file.Close()
	var info windows.ByHandleFileInformation
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, securityFields)
	if err != nil || !ownedDescriptor(sd) || windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, ErrUnsafe
	}
	policy, _, err := accessPolicy(sd)
	if err != nil || (len(policy) > 8 && policy[9]&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE|windows.INHERIT_ONLY_ACE|windows.NO_PROPAGATE_INHERIT_ACE) != 0) {
		return nil, ErrUnsafe
	}
	return file.Stat()
}

func nativeOpen(path string, writable bool) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, ErrUnsafe
	}
	access := uint32(windows.GENERIC_READ | windows.READ_CONTROL)
	if writable {
		access |= windows.GENERIC_WRITE | windows.DELETE
	}
	h, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func nativeMetadata(file *os.File) (nativeAccess, error) {
	h := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	kind, err := windows.GetFileType(h)
	if err != nil || kind != windows.FILE_TYPE_DISK || windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return nativeAccess{}, ErrUnsafe
	}
	// Unsupported storage attributes may change data/access semantics. Do not
	// silently drop encryption, compression, sparse or integrity information.
	const supported = windows.FILE_ATTRIBUTE_ARCHIVE | windows.FILE_ATTRIBUTE_HIDDEN | windows.FILE_ATTRIBUTE_SYSTEM | windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED | windows.FILE_ATTRIBUTE_READONLY | windows.FILE_ATTRIBUTE_NORMAL
	if info.FileAttributes & ^uint32(supported) != 0 {
		return nativeAccess{}, ErrUnsafe
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, securityFields)
	if err != nil {
		return nativeAccess{}, ErrUnsafe
	}
	// ARCHIVE is data-write bookkeeping, NORMAL means no other attributes.
	attributes := info.FileAttributes & ^uint32(windows.FILE_ATTRIBUTE_ARCHIVE|windows.FILE_ATTRIBUTE_NORMAL)
	return accessFromDescriptor(sd, attributes)
}

func accessFromDescriptor(sd *windows.SECURITY_DESCRIPTOR, attributes uint32) (nativeAccess, error) {
	if !ownedDescriptor(sd) {
		return nativeAccess{}, ErrUnsafe
	}
	group, _, err := sd.Group()
	if err != nil || group == nil || !group.IsValid() {
		return nativeAccess{}, ErrUnsafe
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return nativeAccess{}, ErrUnsafe
	}
	control, _, err := sd.Control()
	if err != nil {
		return nativeAccess{}, ErrUnsafe
	}
	policy, present, err := accessPolicy(sd)
	if err != nil {
		return nativeAccess{}, ErrUnsafe
	}
	value := sd.String()
	if value == "" {
		return nativeAccess{}, ErrUnsafe
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return nativeAccess{}, ErrUnsafe
	}
	// ACL header size is a WORD at offset 2 in the native ACL layout.
	header := unsafe.Slice((*byte)(unsafe.Pointer(acl)), 8)
	length := int(header[2]) | int(header[3])<<8
	if length < 8 {
		return nativeAccess{}, ErrUnsafe
	}
	dacl := string(unsafe.Slice((*byte)(unsafe.Pointer(acl)), length))
	return nativeAccess{descriptor: value, owner: owner.String(), group: group.String(), dacl: dacl, control: control, attributes: attributes, policy: policy, policyPresent: present}, nil
}

func accessPolicy(sd *windows.SECURITY_DESCRIPTOR) (string, bool, error) {
	control, _, err := sd.Control()
	if err != nil {
		return "", false, ErrUnsafe
	}
	acl, _, err := sd.SACL()
	present := control&windows.SE_SACL_PRESENT != 0
	if !present && err == windows.ERROR_OBJECT_NOT_FOUND {
		return "", false, nil
	}
	if err != nil {
		return "", false, ErrUnsafe
	}
	if acl == nil {
		return "", present, nil
	}
	header := unsafe.Slice((*byte)(unsafe.Pointer(acl)), 8)
	length := int(binary.LittleEndian.Uint16(header[2:]))
	if length < 8 {
		return "", false, ErrUnsafe
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(acl)), length)
	count := binary.LittleEndian.Uint16(data[4:])
	if count == 0 {
		return string(data), present, nil
	}
	// Only a single well-formed mandatory label is supported. Resource
	// attributes, scoped policy IDs, unknown ACEs and mixed ACLs fail closed.
	if count != 1 || length < 28 || data[8] != 17 || data[9] & ^byte(0x1f) != 0 || binary.LittleEndian.Uint16(data[10:]) != 20 ||
		binary.LittleEndian.Uint32(data[12:]) & ^uint32(7) != 0 || string(data[16:24]) != "\x01\x01\x00\x00\x00\x00\x00\x10" {
		return "", false, ErrUnsafe
	}
	return string(data), present, nil
}

func nativeCreate(path string, newTarget bool) (*os.File, error) {
	if _, err := nativeParent(filepath.Dir(path)); err != nil {
		return nil, ErrUnsafe
	}
	sd, err := creationDescriptor(newTarget)
	if err != nil {
		return nil, ErrUnsafe
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, ErrUnsafe
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE|windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	runtime.KeepAlive(sd)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(h), path)
	identity, statErr := file.Stat()
	fail := func() (*os.File, error) {
		file.Close()
		if statErr == nil {
			cleanupTemporary(path, identity)
		}
		return nil, ErrUnsafe
	}
	if statErr != nil {
		return fail()
	}
	if newTarget {
		label, _, err := sd.SACL()
		if err != nil || windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, label) != nil {
			return fail()
		}
		runtime.KeepAlive(sd)
	}
	actual, err := nativeMetadata(file)
	if err != nil || actual.control&windows.SE_DACL_PROTECTED == 0 {
		return fail()
	}
	return file, nil
}

func nativeCopySecurity(source, destination *os.File, access nativeAccess) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(source.Fd()), windows.SE_FILE_OBJECT, securityFields)
	if err != nil || !ownedDescriptor(sd) {
		return ErrUnsafe
	}
	captured, err := accessFromDescriptor(sd, access.attributes)
	if err != nil || captured != access {
		return ErrUnsafe
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return ErrUnsafe
	}
	group, _, err := sd.Group()
	if err != nil {
		return ErrUnsafe
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return ErrUnsafe
	}
	flags := windows.SECURITY_INFORMATION(copySecurityFields)
	if access.control&windows.SE_DACL_PROTECTED != 0 {
		flags |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		flags |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetSecurityInfo(windows.Handle(destination.Fd()), windows.SE_FILE_OBJECT, flags, owner, group, acl, nil); err != nil {
		return ErrUnsafe
	}
	if len(access.policy) > 8 && access.policy[4] == 1 {
		label, _, err := sd.SACL()
		if err != nil || windows.SetSecurityInfo(windows.Handle(destination.Fd()), windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, label) != nil {
			return ErrUnsafe
		}
	}
	// No label initialization/clearing for absent-label sources: clearing can
	// leave SACL control bits behind, which is not a Ruling44 exception.
	runtime.KeepAlive(sd)
	// Set only the newly created temporary object's validated attributes.
	return setTemporaryAttributes(destination, access.attributes)
}

func setTemporaryAttributes(file *os.File, attributes uint32) error {
	type basicInformation struct {
		CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
		FileAttributes                                          uint32
	}
	if attributes == 0 {
		attributes = windows.FILE_ATTRIBUTE_NORMAL
	}
	info := basicInformation{FileAttributes: attributes}
	return windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileBasicInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
}

func nativeReplace(from, to string, identity os.FileInfo, access nativeAccess) error {
	file, err := nativeOpen(from, true)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(identity, info) {
		return ErrUnsafe
	}
	actual, err := nativeMetadata(file)
	if err != nil || actual != access {
		return ErrUnsafe
	}
	return winfile.Replace(windows.Handle(file.Fd()), to)
}
