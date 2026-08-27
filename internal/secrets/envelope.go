package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"unicode/utf8"
)

type envelope struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Backend        string `json:"backend"`
	InstallationID string `json:"installationId"`
	Purpose        string `json:"purpose"`
	Ciphertext     []byte `json:"ciphertext"`
}

func scope(id, purpose, backend string) []byte {
	b, _ := json.Marshal([]any{"agent-task-notify", 1, id, purpose, backend})
	return b
}

func marshalEnvelope(id, purpose, backend string, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 || len(ciphertext) > maxEnvelope {
		return nil, ErrUnavailable
	}
	b, err := json.Marshal(envelope{1, backend, id, purpose, ciphertext})
	if err != nil || len(b) > maxEnvelope {
		return nil, ErrUnavailable
	}
	return b, nil
}

// Decode each exact field once. encoding/json's struct decoder by itself would
// accept duplicate keys and case-insensitive field names, which are forbidden.
func parseEnvelope(data []byte) (envelope, error) {
	var env envelope
	if len(data) == 0 || len(data) > maxEnvelope || !utf8.Valid(data) {
		return env, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return env, ErrInvalid
	}
	seen := make(map[string]bool, 5)
	for dec.More() {
		token, err = dec.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return env, ErrInvalid
		}
		seen[key] = true
		var raw json.RawMessage
		if dec.Decode(&raw) != nil || bytes.Equal(raw, []byte("null")) {
			return env, ErrInvalid
		}
		switch key {
		case "schemaVersion":
			err = json.Unmarshal(raw, &env.SchemaVersion)
		case "backend":
			err = json.Unmarshal(raw, &env.Backend)
		case "installationId":
			err = json.Unmarshal(raw, &env.InstallationID)
		case "purpose":
			err = json.Unmarshal(raw, &env.Purpose)
		case "ciphertext":
			var encoded string
			err = json.Unmarshal(raw, &encoded)
			if err == nil {
				env.Ciphertext, err = base64.StdEncoding.Strict().DecodeString(encoded)
				if err == nil && base64.StdEncoding.EncodeToString(env.Ciphertext) != encoded {
					return env, ErrInvalid
				}
			}
		default:
			return env, ErrInvalid
		}
		if err != nil {
			return env, ErrInvalid
		}
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') {
		return env, ErrInvalid
	}
	if _, err = dec.Token(); err != io.EOF {
		return env, ErrInvalid
	}
	if len(seen) != 5 || env.SchemaVersion != 1 || (env.Backend != "dpapi" && env.Backend != "keychain-aes-gcm") || !validID(env.InstallationID) || !validPurpose(env.Purpose) || len(env.Ciphertext) == 0 {
		return env, ErrInvalid
	}
	return env, nil
}

type aesBackend struct{ key []byte }

func (*aesBackend) name() string { return "keychain-aes-gcm" }

func (b *aesBackend) aead() (cipher.AEAD, error) {
	if len(b.key) != 32 {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return nil, ErrUnavailable
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	return gcm, nil
}

func (b *aesBackend) protect(plain, aad []byte) ([]byte, error) {
	gcm, err := b.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, ErrUnavailable
	}
	return gcm.Seal(nonce, nonce, plain, aad), nil
}

func (b *aesBackend) unprotect(sealed, aad []byte) ([]byte, error) {
	gcm, err := b.aead()
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrIntegrity
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], aad)
	if err != nil {
		return nil, ErrIntegrity
	}
	return plain, nil
}
