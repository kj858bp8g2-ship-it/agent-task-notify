// Package secrets protects installation-scoped credentials and original backups.
// Errors deliberately contain no caller data or native diagnostic text.
package secrets

import "errors"

type AccessMode uint8

const (
	Foreground AccessMode = iota
	Background
)

var (
	ErrInvalid     = errors.New("invalid protected data")
	ErrUnavailable = errors.New("secure storage unavailable")
	ErrIntegrity   = errors.New("protected data integrity failure")
)

const (
	maxPlaintext = 4 << 20
	maxEnvelope  = 6 << 20
)

type protector interface {
	name() string
	protect([]byte, []byte) ([]byte, error)
	unprotect([]byte, []byte) ([]byte, error)
}

type Vault struct {
	installationID string
	backend        protector
}

func Open(installationID string, mode AccessMode) (*Vault, error) {
	if !validID(installationID) || (mode != Foreground && mode != Background) {
		return nil, ErrInvalid
	}
	backend, err := openNative(installationID, mode)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Vault{installationID: installationID, backend: backend}, nil
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validPurpose(purpose string) bool {
	switch purpose {
	case "credential:bark", "credential:ntfy", "backup:codex", "backup:claude-code", "backup:cursor", "backup:gemini-cli", "backup:opencode", "backup:workbuddy":
		return true
	}
	return false
}

func (v *Vault) valid(purpose string) bool {
	return v != nil && v.backend != nil && validID(v.installationID) && validPurpose(purpose)
}

func (v *Vault) Protect(purpose string, plaintext []byte) ([]byte, error) {
	if !v.valid(purpose) || len(plaintext) > maxPlaintext {
		return nil, ErrInvalid
	}
	aad := scope(v.installationID, purpose, v.backend.name())
	ciphertext, err := v.backend.protect(plaintext, aad)
	if err != nil {
		return nil, err
	}
	defer clear(ciphertext)
	return marshalEnvelope(v.installationID, purpose, v.backend.name(), ciphertext)
}

func (v *Vault) Unprotect(purpose string, data []byte) ([]byte, error) {
	if !v.valid(purpose) {
		return nil, ErrInvalid
	}
	env, err := parseEnvelope(data)
	if err != nil {
		return nil, err
	}
	defer clear(env.Ciphertext)
	if env.InstallationID != v.installationID || env.Purpose != purpose || env.Backend != v.backend.name() {
		return nil, ErrIntegrity
	}
	plain, err := v.backend.unprotect(env.Ciphertext, scope(v.installationID, purpose, v.backend.name()))
	if err != nil {
		return nil, err
	}
	if len(plain) > maxPlaintext {
		clear(plain)
		return nil, ErrInvalid
	}
	return plain, nil
}
