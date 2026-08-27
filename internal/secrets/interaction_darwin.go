package secrets

/*
#cgo LDFLAGS: -framework Security
#include <Security/Security.h>

static OSStatus atn_get_interaction(Boolean *allowed) {
    return SecKeychainGetUserInteractionAllowed(allowed);
}
static OSStatus atn_set_interaction(Boolean allowed) {
    return SecKeychainSetUserInteractionAllowed(allowed);
}
*/
import "C"

import "sync"

// The Security interaction setting is process-global. Every package Keychain
// operation must hold this lock through restoration, including error paths.
var interactionMu sync.Mutex

func withInteraction(mode AccessMode, operation func() error) (err error) {
	interactionMu.Lock()
	defer interactionMu.Unlock()
	var previous C.Boolean
	if C.atn_get_interaction(&previous) != 0 {
		return ErrUnavailable
	}
	if mode == Background {
		// Restore even when setting false reports failure: do not assume a
		// failed native call left the process-global state untouched.
		defer func() {
			if C.atn_set_interaction(previous) != 0 {
				err = ErrUnavailable
			}
		}()
		if C.atn_set_interaction(0) != 0 {
			return ErrUnavailable
		}
	}
	// Foreground respects a preexisting disabled state; it never enables UI.
	return operation()
}
