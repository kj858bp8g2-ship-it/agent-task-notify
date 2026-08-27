package secrets

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type dpapiBackend struct{}

func openNative(_ string, _ AccessMode) (protector, error) { return dpapiBackend{}, nil }
func (dpapiBackend) name() string                          { return "dpapi" }

func (dpapiBackend) protect(plain, aad []byte) ([]byte, error) {
	// A version byte distinguishes a valid empty backup from invalid empty
	// native output. It is inside DPAPI authentication, never a plaintext file.
	framed := make([]byte, 1+len(plain))
	framed[0] = 1
	copy(framed[1:], plain)
	defer clear(framed)
	return cryptDPAPI(framed, aad, false)
}

func (dpapiBackend) unprotect(sealed, aad []byte) ([]byte, error) {
	framed, err := cryptDPAPI(sealed, aad, true)
	if err != nil {
		return nil, err
	}
	defer clear(framed)
	if len(framed) == 0 || framed[0] != 1 {
		return nil, ErrIntegrity
	}
	return append([]byte{}, framed[1:]...), nil
}

func cryptDPAPI(data, aad []byte, decrypt bool) ([]byte, error) {
	input := windows.DataBlob{Size: uint32(len(data))}
	if len(data) > 0 {
		input.Data = &data[0]
	}
	entropy := windows.DataBlob{Size: uint32(len(aad))}
	if len(aad) > 0 {
		entropy.Data = &aad[0]
	}
	var output windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	} else {
		// CurrentUser scope: intentionally never CRYPTPROTECT_LOCAL_MACHINE.
		err = windows.CryptProtectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	runtime.KeepAlive(data)
	runtime.KeepAlive(aad)
	if output.Data != nil {
		defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	}
	if err != nil {
		if decrypt {
			return nil, ErrIntegrity
		}
		return nil, ErrUnavailable
	}
	limit := maxEnvelope
	if decrypt {
		limit = maxPlaintext + 1
	}
	if output.Data == nil || output.Size == 0 || uint64(output.Size) > uint64(limit) {
		return nil, ErrIntegrity
	}
	native := unsafe.Slice(output.Data, int(output.Size))
	defer clear(native)
	return append([]byte(nil), native...), nil
}
