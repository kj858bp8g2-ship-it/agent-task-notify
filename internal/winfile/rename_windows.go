// Package winfile supplies the permission-agnostic native rename operation.
package winfile

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Replace atomically advances target to the caller-owned, validated handle.
// The caller must validate its own access and identity contract before calling.
func Replace(h windows.Handle, to string) error {
	target, err := windows.UTF16FromString(to)
	if err != nil {
		return err
	}
	// Native pointer alignment supplies the correct HANDLE padding on each arch.
	type renameInformation struct {
		Flags          uint32
		RootDirectory  windows.Handle
		FileNameLength uint32
		FileName       [1]uint16
	}
	var layout renameInformation
	offset := unsafe.Offsetof(layout.FileName)
	buffer := make([]byte, int(unsafe.Sizeof(layout))+len(target)*2)
	header := (*renameInformation)(unsafe.Pointer(&buffer[0]))
	header.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	header.FileNameLength = uint32((len(target) - 1) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[offset])), len(target)), target)
	// POSIX replacement preserves old shared handles and advances the name.
	// Unsupported APIs/filesystems fail closed; never fall back to MoveFileEx.
	// Non-delete-sharing readers still block. Keep the same 200ms wait budget.
	for attempt := 0; attempt < 21; attempt++ {
		err = windows.SetFileInformationByHandle(h, windows.FileRenameInfoEx, &buffer[0], uint32(len(buffer)))
		if err == nil {
			return nil
		}
		if attempt == 20 || (!errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED)) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return err
}
