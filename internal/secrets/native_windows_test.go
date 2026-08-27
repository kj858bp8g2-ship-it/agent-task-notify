package secrets

import (
	"encoding/json"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func cleanupSyntheticAccount(t *testing.T, id string) { t.Helper() }

func TestWindowsBackgroundReopensSyntheticEnvelope(t *testing.T) {
	v, id := syntheticVault(t)
	b, err := v.Protect("credential:ntfy", []byte("synthetic"))
	if err != nil {
		t.Fatal("protection failed")
	}
	w, err := Open(id, Background)
	if err != nil {
		t.Fatal("background vault unavailable")
	}
	got, err := w.Unprotect("credential:ntfy", b)
	if err != nil || string(got) != "synthetic" {
		t.Fatal("background roundtrip failed")
	}
}

func TestWindowsRejectsMissingAndWrongInternalFrame(t *testing.T) {
	v, id := syntheticVault(t)
	aad := []byte(`["agent-task-notify",1,"` + id + `","backup:codex","dpapi"]`)
	// CryptProtectData rejects a zero-length input itself. Nonempty unframed
	// and unsupported-version native plaintext exercise our own frame check.
	for _, plain := range [][]byte{{2}, []byte("synthetic-unframed")} {
		input := windows.DataBlob{Size: uint32(len(plain))}
		if len(plain) > 0 {
			input.Data = &plain[0]
		}
		entropy := windows.DataBlob{Size: uint32(len(aad)), Data: &aad[0]}
		var output windows.DataBlob
		err := windows.CryptProtectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
		runtime.KeepAlive(plain)
		runtime.KeepAlive(aad)
		if err != nil {
			t.Fatal("synthetic native protection failed")
		}
		if output.Data == nil || output.Size == 0 {
			t.Fatal("synthetic native output missing")
		}
		sealed := append([]byte(nil), unsafe.Slice(output.Data, int(output.Size))...)
		_, err = windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
		if err != nil {
			t.Fatal("synthetic native allocation cleanup failed")
		}
		b, _ := json.Marshal(map[string]any{"schemaVersion": 1, "backend": "dpapi", "installationId": id, "purpose": "backup:codex", "ciphertext": sealed})
		_, err = v.Unprotect("backup:codex", b)
		requireSafeError(t, err)
	}
}
