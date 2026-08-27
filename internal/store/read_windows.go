package store

import (
	"os"

	"golang.org/x/sys/windows"
)

func nativeReadOpen(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, errPrivate
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, errPrivate
	}
	file := os.NewFile(uintptr(h), path)
	if !nativeHandlePrivate(file, false) {
		file.Close()
		return nil, errPrivate
	}
	return file, nil
}

func nativeHandlePrivate(file *os.File, directory bool) bool {
	h := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil || !descriptorPrivate(sd) || windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	kind, err := windows.GetFileType(h)
	return err == nil && kind == windows.FILE_TYPE_DISK && (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) == directory
}
