package store

/*
#include <sys/types.h>
#include <sys/acl.h>
#include <sys/stat.h>
#include <fcntl.h>
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

// acl_get_* collapses a successful stat with no ACL property into NULL/ENOENT.
// Query the property only after a successful extended stat instead: a missing
// path, invalid fd, denied read, or unsupported operation must still fail closed.
static int notify_filesec_empty(filesec_t security) {
    int present = 0;
    if (filesec_query_property(security, FILESEC_ACL, &present) != 0) return 0;
    if (!present) return 1;
    acl_t acl = NULL;
    if (filesec_get_property(security, FILESEC_ACL, &acl) != 0) return 0;
    return notify_acl_empty(acl);
}

static int notify_path_acl_empty(const char *path) {
    filesec_t security = filesec_init();
    if (security == NULL) return 0;
    struct stat st;
    int result = lstatx_np(path, &st, security) == 0 &&
        (S_ISREG(st.st_mode) || S_ISDIR(st.st_mode)) && notify_filesec_empty(security);
    filesec_free(security);
    return result;
}

static int notify_fd_acl_empty(int fd) {
    filesec_t security = filesec_init();
    if (security == NULL) return 0;
    struct stat st;
    int result = fstatx_np(fd, &st, security) == 0 &&
        (S_ISREG(st.st_mode) || S_ISDIR(st.st_mode)) && notify_filesec_empty(security);
    filesec_free(security);
    return result;
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
