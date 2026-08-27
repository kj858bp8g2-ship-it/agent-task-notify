package store

/*
#include <sys/types.h>
#include <sys/acl.h>
#include <errno.h>
#include <stdlib.h>

// Darwin returns 0 for an entry and -1/EINVAL at the end, unlike Linux.
// Validate the successfully retrieved ACL first; never treat a retrieval,
// validation, or unsupported-filesystem error as an empty ACL.
static int notify_acl_empty(acl_t acl) {
    if (acl == NULL) return 0;
    if (acl_valid(acl) != 0) { acl_free(acl); return 0; }
    acl_entry_t entry;
    errno = 0;
    int result = acl_get_entry(acl, ACL_FIRST_ENTRY, &entry);
    int saved_errno = errno;
    acl_free(acl);
    return result == -1 && saved_errno == EINVAL;
}

static int notify_path_acl_empty(const char *path) {
    return notify_acl_empty(acl_get_link_np(path, ACL_TYPE_EXTENDED));
}

static int notify_fd_acl_empty(int fd) {
    return notify_acl_empty(acl_get_fd_np(fd, ACL_TYPE_EXTENDED));
}

static int notify_clear_new_acl(int fd) {
    acl_t empty = acl_init(0);
    if (empty == NULL) return 0;
    int result = acl_set_fd_np(fd, empty, ACL_TYPE_EXTENDED);
    acl_free(empty);
    return result == 0 && notify_fd_acl_empty(fd);
}
*/
import "C"

import "unsafe"

func emptyPathACL(path string) bool {
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	return C.notify_path_acl_empty(value) == 1
}

func emptyFileACL(fd int) bool { return C.notify_fd_acl_empty(C.int(fd)) == 1 }

// Called only for an object successfully created exclusively by this invocation.
func clearNewFileACL(fd int) bool { return C.notify_clear_new_acl(C.int(fd)) == 1 }
