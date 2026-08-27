package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests catch plaintext storage, lost authentication/scope, permissive
// parsing and path-bound encryption by exercising the real platform backend.
func syntheticID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal("synthetic identity unavailable")
	}
	return hex.EncodeToString(b)
}

func syntheticVault(t *testing.T) (*Vault, string) {
	t.Helper()
	id := syntheticID(t)
	cleanupSyntheticAccount(t, id)
	v, err := Open(id, Foreground)
	if err != nil {
		t.Fatal("synthetic vault unavailable")
	}
	return v, id
}

func requireSafeError(t *testing.T, err error) {
	t.Helper()
	if err != ErrInvalid && err != ErrUnavailable && err != ErrIntegrity {
		t.Fatal("expected a static safe package error")
	}
}

func TestNativeRoundtripAndPurposeBinding(t *testing.T) {
	v, _ := syntheticVault(t)
	plain := []byte("synthetic notification credential 中文")
	sealed, err := v.Protect("credential:bark", plain)
	if err != nil || bytes.Contains(sealed, plain) {
		t.Fatal("not protected")
	}
	got, err := v.Unprotect("credential:bark", sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("roundtrip failed")
	}
	_, err = v.Unprotect("credential:ntfy", sealed)
	requireSafeError(t, err)
}

func TestSeparateProtectCallsDiffer(t *testing.T) {
	v, _ := syntheticVault(t)
	a, err := v.Protect("credential:bark", []byte("synthetic"))
	if err != nil {
		t.Fatal("protection failed")
	}
	b, err := v.Protect("credential:bark", []byte("synthetic"))
	if err != nil || bytes.Equal(a, b) {
		t.Fatal("ciphertext must be randomized")
	}
}

func TestChangedCiphertextAndInstallationRejected(t *testing.T) {
	v, _ := syntheticVault(t)
	b, err := v.Protect("credential:bark", []byte("synthetic"))
	if err != nil {
		t.Fatal("protection failed")
	}
	var fields map[string]any
	if json.Unmarshal(b, &fields) != nil {
		t.Fatal("invalid output")
	}
	c, err := base64.StdEncoding.DecodeString(fields["ciphertext"].(string))
	if err != nil || len(c) == 0 {
		t.Fatal("missing ciphertext")
	}
	c[len(c)/2] ^= 1
	fields["ciphertext"] = base64.StdEncoding.EncodeToString(c)
	tampered, _ := json.Marshal(fields)
	_, err = v.Unprotect("credential:bark", tampered)
	requireSafeError(t, err)
	if json.Unmarshal(b, &fields) != nil {
		t.Fatal("invalid output")
	}
	other, otherID := syntheticVault(t)
	_, err = other.Unprotect("credential:bark", b)
	requireSafeError(t, err)
	fields["installationId"] = otherID
	tampered, _ = json.Marshal(fields)
	_, err = other.Unprotect("credential:bark", tampered)
	requireSafeError(t, err)
	fields["purpose"] = "credential:ntfy"
	tampered, _ = json.Marshal(fields)
	_, err = other.Unprotect("credential:ntfy", tampered)
	requireSafeError(t, err)
	if json.Unmarshal(b, &fields) != nil {
		t.Fatal("invalid output")
	}
	fields["purpose"] = "credential:ntfy"
	tampered, _ = json.Marshal(fields)
	_, err = v.Unprotect("credential:ntfy", tampered)
	requireSafeError(t, err)
}

func TestEncryptedBackupMovesBetweenStagingPaths(t *testing.T) {
	v, _ := syntheticVault(t)
	b, err := v.Protect("backup:codex", []byte("synthetic original config 中文"))
	if err != nil {
		t.Fatal("protection failed")
	}
	a := filepath.Join(t.TempDir(), "first.envelope")
	z := filepath.Join(t.TempDir(), "second.envelope")
	if os.WriteFile(a, b, 0600) != nil {
		t.Fatal("staging failed")
	}
	c, err := os.ReadFile(a)
	if err != nil || os.WriteFile(z, c, 0600) != nil {
		t.Fatal("copy failed")
	}
	c, err = os.ReadFile(z)
	if err != nil {
		t.Fatal("copy read failed")
	}
	got, err := v.Unprotect("backup:codex", c)
	if err != nil || string(got) != "synthetic original config 中文" {
		t.Fatal("moving envelope broke decryption")
	}
}

func TestAllPurposesAndEmptyBackup(t *testing.T) {
	v, _ := syntheticVault(t)
	for _, purpose := range []string{"credential:bark", "credential:ntfy", "backup:codex", "backup:claude-code", "backup:cursor", "backup:gemini-cli", "backup:opencode", "backup:workbuddy"} {
		b, err := v.Protect(purpose, nil)
		if err != nil {
			t.Fatal("empty protection failed")
		}
		got, err := v.Unprotect(purpose, b)
		if err != nil || len(got) != 0 {
			t.Fatal("empty roundtrip failed")
		}
	}
}

func TestInvalidIdentityModePurposeAndSafeDiagnostics(t *testing.T) {
	for _, id := range []string{"", "synthetic secret 中文", strings.Repeat("A", 32), strings.Repeat("g", 32), strings.Repeat("1", 31), strings.Repeat("1", 33)} {
		_, err := Open(id, Foreground)
		requireSafeError(t, err)
	}
	_, err := Open(syntheticID(t), AccessMode(99))
	requireSafeError(t, err)
	v, _ := syntheticVault(t)
	for _, purpose := range []string{"", "backup:unknown", "credential:unknown", "synthetic secret 中文"} {
		_, err := v.Protect(purpose, []byte("synthetic secret 中文"))
		requireSafeError(t, err)
		_, err = v.Unprotect(purpose, []byte("synthetic secret 中文"))
		requireSafeError(t, err)
	}
	for _, invalid := range []*Vault{nil, {}} {
		_, err := invalid.Protect("credential:bark", nil)
		requireSafeError(t, err)
		_, err = invalid.Unprotect("credential:bark", nil)
		requireSafeError(t, err)
	}
}

func TestRejectWrongDuplicateTrailingAndMalformedEnvelope(t *testing.T) {
	v, _ := syntheticVault(t)
	b, err := v.Protect("credential:bark", []byte("synthetic"))
	if err != nil {
		t.Fatal("protection failed")
	}
	for _, field := range []string{"schemaVersion", "backend", "installationId", "purpose", "ciphertext"} {
		var fields map[string]json.RawMessage
		if json.Unmarshal(b, &fields) != nil {
			t.Fatal("invalid output")
		}
		duplicate := append([]byte(`{"`+field+`":`+string(fields[field])+`,`), b[1:]...)
		_, err := v.Unprotect("credential:bark", duplicate)
		requireSafeError(t, err)
		delete(fields, field)
		missing, _ := json.Marshal(fields)
		_, err = v.Unprotect("credential:bark", missing)
		requireSafeError(t, err)
	}
	for _, input := range [][]byte{nil, []byte("null"), []byte("[]"), []byte("{"), append(bytes.Clone(b), []byte("{}")...), append([]byte(`{"unknown":1,`), b[1:]...), append([]byte(`{"SchemaVersion":1,`), b[1:]...), []byte("synthetic secret 中文")} {
		_, err := v.Unprotect("credential:bark", input)
		requireSafeError(t, err)
	}
	for field, values := range map[string][]any{
		"schemaVersion": {0, 2, "1", nil, 1.5}, "backend": {"unknown", nil, 3},
		"installationId": {nil, 3, "invalid"}, "purpose": {nil, 3, "backup:unknown"},
		"ciphertext": {nil, 3, "", "%%%", "AA==\n"},
	} {
		for _, value := range values {
			var fields map[string]any
			if json.Unmarshal(b, &fields) != nil {
				t.Fatal("invalid output")
			}
			fields[field] = value
			input, _ := json.Marshal(fields)
			_, err := v.Unprotect("credential:bark", input)
			requireSafeError(t, err)
		}
	}
}

func TestPlaintextAndEnvelopeSizeLimits(t *testing.T) {
	v, _ := syntheticVault(t)
	plain := bytes.Repeat([]byte("x"), 4<<20)
	b, err := v.Protect("backup:codex", plain)
	if err != nil {
		t.Fatal("maximum plaintext rejected")
	}
	got, err := v.Unprotect("backup:codex", b)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("maximum roundtrip failed")
	}
	_, err = v.Protect("backup:codex", append(plain, 'x'))
	requireSafeError(t, err)
	_, err = v.Unprotect("backup:codex", make([]byte, (6<<20)+1))
	requireSafeError(t, err)
}

func TestAESGCMUsesCanonicalScopeAndNoncePrefix(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal("synthetic key unavailable")
	}
	id := syntheticID(t)
	v := &Vault{installationID: id, backend: &aesBackend{key: key}}
	b, err := v.Protect("backup:codex", []byte("synthetic"))
	if err != nil {
		t.Fatal("AES protection failed")
	}
	var fields map[string]any
	if json.Unmarshal(b, &fields) != nil {
		t.Fatal("invalid envelope")
	}
	c, err := base64.StdEncoding.DecodeString(fields["ciphertext"].(string))
	if err != nil {
		t.Fatal("invalid ciphertext")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal("synthetic cipher failed")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal("synthetic cipher failed")
	}
	aad := []byte(`["agent-task-notify",1,"` + id + `","backup:codex","keychain-aes-gcm"]`)
	if len(c) < gcm.NonceSize() {
		t.Fatal("nonce missing")
	}
	got, err := gcm.Open(nil, c[:gcm.NonceSize()], c[gcm.NonceSize():], aad)
	if err != nil || string(got) != "synthetic" {
		t.Fatal("canonical AAD or nonce layout incorrect")
	}
	got, err = v.Unprotect("backup:codex", b)
	if err != nil || string(got) != "synthetic" {
		t.Fatal("AES roundtrip failed")
	}
	c[len(c)-1] ^= 1
	fields["ciphertext"] = base64.StdEncoding.EncodeToString(c)
	b, _ = json.Marshal(fields)
	_, err = v.Unprotect("backup:codex", b)
	requireSafeError(t, err)
}
