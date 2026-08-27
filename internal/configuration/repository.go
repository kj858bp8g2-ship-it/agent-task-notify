package configuration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/providers"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/secrets"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

var (
	errConfigurationUnavailable = errors.New("configuration unavailable")
	errConfigurationInvalid     = errors.New("invalid configuration")
)

type bundle struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Settings      core.Settings              `json:"settings"`
	Credentials   map[string]json.RawMessage `json:"credentials"`
}
type installation struct {
	SchemaVersion  int    `json:"schemaVersion"`
	InstallationID string `json:"installationId"`
}

func (r *Repository) Settings() (core.Settings, error) {
	state, found, err := r.readBundle()
	if err != nil {
		return core.Settings{}, err
	}
	if found {
		if _, err := r.checkCredentials(state); err != nil {
			return core.Settings{}, err
		}
	}
	return state.Settings, nil
}

// Credential is strictly read-only in either mode. Mode is an access ceiling,
// not a promise of interaction: even Foreground uses Background key access.
// Creation/authorization requires Configure or an explicit Vault(Foreground).
func (r *Repository) Credential(provider string, mode secrets.AccessMode) (providers.Credential, error) {
	if !validProvider(provider) || !validMode(mode) {
		return providers.Credential{}, errConfigurationInvalid
	}
	state, found, err := r.readBundle()
	if err != nil || !found {
		return providers.Credential{}, errConfigurationUnavailable
	}
	envelope, ok := state.Credentials[provider]
	if !ok {
		return providers.Credential{}, errConfigurationUnavailable
	}
	id, err := r.readIdentity()
	if err != nil {
		return providers.Credential{}, errConfigurationUnavailable
	}
	// First require an existing key even for foreground reads; secrets.Open in
	// Foreground is otherwise allowed to create a new platform key.
	vault, err := secrets.Open(id, secrets.Background)
	if err != nil {
		return providers.Credential{}, errConfigurationUnavailable
	}
	return decodeCredential(vault, provider, envelope)
}

func validProvider(provider string) bool { return provider == "bark" || provider == "ntfy" }
func validMode(mode secrets.AccessMode) bool {
	return mode == secrets.Foreground || mode == secrets.Background
}

func (r *Repository) Configure(ctx context.Context, provider string, credential providers.Credential, settingsPatch []byte) error {
	return r.configure(ctx, provider, credential, settingsPatch, r.vaultLocked)
}

// The public entry always supplies the installation-scoped, lock-held opener.
// A per-call parameter keeps mode sequencing testable without global switches
// or replacing the real Vault, encryption, locks or atomic filesystem commit.
func (r *Repository) configure(ctx context.Context, provider string, credential providers.Credential, settingsPatch []byte, openVault func(secrets.AccessMode) (*secrets.Vault, error)) error {
	if ctx == nil || !validProvider(provider) || !utf8.ValidString(credential.Endpoint) || !utf8.ValidString(credential.Token) || providers.ValidateCredential(provider, credential) != nil {
		return errConfigurationInvalid
	}
	patch, err := strictjson.Object(settingsPatch)
	if err != nil {
		return errConfigurationInvalid
	}
	if raw, ok := patch["provider"]; ok {
		selected, err := strictjson.String(raw)
		if err != nil || selected != provider {
			return errConfigurationInvalid
		}
	}
	if err := r.Prepare(); err != nil {
		return err
	}
	release, err := r.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	state, found, err := r.readBundle()
	if err != nil {
		return err
	}
	state.Settings.Provider = provider
	settings, err := core.ParseSettings(settingsPatch, state.Settings)
	if err != nil {
		return errConfigurationInvalid
	}
	plain, err := json.Marshal(credential)
	if err != nil {
		return errConfigurationInvalid
	}
	defer clear(plain)
	// Configure already holds configuration.lock; never call public Vault here.
	vault, err := openVault(secrets.Foreground)
	if err != nil {
		return err
	}
	// A deliberate foreground configuration must be able to authorize the
	// existing key. Reuse this one vault for old-envelope authentication and
	// the new protection; a Background preflight would prevent authorization.
	if found {
		if err := checkCredentialsWithVault(state, vault); err != nil {
			return err
		}
	}
	envelope, err := vault.Protect("credential:"+provider, plain)
	if err != nil {
		return errConfigurationUnavailable
	}
	defer clear(envelope)
	state.Settings = settings
	state.Credentials[provider] = envelope
	encoded, err := json.Marshal(state)
	if err != nil || len(encoded) > strictjson.MaxBytes {
		return errConfigurationInvalid
	}
	defer clear(encoded)
	if ctx.Err() != nil || store.WriteAtomic(filepath.Join(r.directory, "configuration.json"), encoded) != nil {
		return errConfigurationUnavailable
	}
	return nil
}

func (r *Repository) acquire(ctx context.Context) (func() error, error) {
	if ctx == nil {
		return nil, errConfigurationInvalid
	}
	lockContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	release, err := store.Acquire(lockContext, filepath.Join(r.directory, "configuration.lock"))
	if err != nil {
		return nil, errConfigurationUnavailable
	}
	return release, nil
}

// Vault supplies the same stable identity for encrypted installer backups.
// No code here acquires an installer lock while holding configuration.lock.
func (r *Repository) Vault(ctx context.Context, mode secrets.AccessMode) (*secrets.Vault, error) {
	if ctx == nil || !validMode(mode) {
		return nil, errConfigurationInvalid
	}
	if ctx.Err() != nil || r.checkDirectory() != nil {
		return nil, errConfigurationUnavailable
	}
	if mode == secrets.Background {
		return r.vaultLocked(mode)
	}
	if err := r.Prepare(); err != nil {
		return nil, err
	}
	release, err := r.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return r.vaultLocked(mode)
}

func (r *Repository) vaultLocked(mode secrets.AccessMode) (*secrets.Vault, error) {
	id, err := r.readIdentity()
	if err == store.ErrNotFound && mode == secrets.Foreground {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, errConfigurationUnavailable
		}
		id = hex.EncodeToString(random[:])
		encoded, err := json.Marshal(installation{1, id})
		if err != nil || store.WriteAtomic(filepath.Join(r.directory, "installation.json"), encoded) != nil {
			return nil, errConfigurationUnavailable
		}
	} else if err != nil {
		return nil, errConfigurationUnavailable
	}
	vault, err := secrets.Open(id, mode)
	if err != nil {
		return nil, errConfigurationUnavailable
	}
	return vault, nil
}

func (r *Repository) readIdentity() (string, error) {
	data, err := store.ReadPrivate(filepath.Join(r.directory, "installation.json"), 1024)
	if err != nil {
		return "", err
	}
	defer clear(data)
	object, err := strictjson.Object(data)
	if err != nil || len(object) != 2 {
		return "", errConfigurationInvalid
	}
	version, err := strictjson.Integer(object["schemaVersion"])
	if err != nil || version != 1 {
		return "", errConfigurationInvalid
	}
	id, err := strictjson.String(object["installationId"])
	if err != nil || !validID(id) {
		return "", errConfigurationInvalid
	}
	return id, nil
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

func (r *Repository) readBundle() (bundle, bool, error) {
	if r.checkDirectory() != nil {
		return bundle{}, false, errConfigurationUnavailable
	}
	defaults, err := core.Defaults()
	if err != nil {
		return bundle{}, false, errConfigurationUnavailable
	}
	empty := bundle{1, defaults, make(map[string]json.RawMessage)}
	if _, err := os.Lstat(r.directory); os.IsNotExist(err) {
		return empty, false, nil
	}
	data, err := store.ReadPrivate(filepath.Join(r.directory, "configuration.json"), strictjson.MaxBytes)
	if err == store.ErrNotFound {
		return empty, false, nil
	}
	if err != nil {
		return bundle{}, false, errConfigurationUnavailable
	}
	defer clear(data)
	object, err := strictjson.Object(data)
	if err != nil || len(object) != 3 {
		return bundle{}, false, errConfigurationInvalid
	}
	version, err := strictjson.Integer(object["schemaVersion"])
	if err != nil || version != 1 {
		return bundle{}, false, errConfigurationInvalid
	}
	snapshot, err := strictjson.Object(object["settings"])
	// All snapshot fields are mandatory; ParseSettings alone accepts patches.
	if err != nil || len(snapshot) != 12 {
		return bundle{}, false, errConfigurationInvalid
	}
	settings, err := core.ParseSettings(object["settings"], defaults)
	if err != nil {
		return bundle{}, false, errConfigurationInvalid
	}
	credentials, err := strictjson.Object(object["credentials"])
	if err != nil || len(credentials) > 2 {
		return bundle{}, false, errConfigurationInvalid
	}
	id, err := r.readIdentity()
	if err != nil {
		return bundle{}, false, errConfigurationUnavailable
	}
	for provider, envelope := range credentials {
		if !validProvider(provider) || !validEnvelope(envelope, id, "credential:"+provider) {
			return bundle{}, false, errConfigurationInvalid
		}
	}
	if _, ok := credentials[settings.Provider]; !ok {
		return bundle{}, false, errConfigurationInvalid
	}
	return bundle{1, settings, credentials}, true, nil
}

// Validate the envelope boundary without treating encrypted bytes as settings.
// Authentication remains the platform Vault's responsibility.
func validEnvelope(data []byte, id, purpose string) bool {
	object, err := strictjson.Object(data)
	if err != nil || len(object) != 5 {
		return false
	}
	version, err := strictjson.Integer(object["schemaVersion"])
	if err != nil || version != 1 {
		return false
	}
	gotID, err := strictjson.String(object["installationId"])
	if err != nil || gotID != id {
		return false
	}
	gotPurpose, err := strictjson.String(object["purpose"])
	if err != nil || gotPurpose != purpose {
		return false
	}
	backend, err := strictjson.String(object["backend"])
	if err != nil || (backend != "dpapi" && backend != "keychain-aes-gcm") {
		return false
	}
	encoded, err := strictjson.String(object["ciphertext"])
	if err != nil || encoded == "" {
		return false
	}
	ciphertext, err := base64.StdEncoding.Strict().DecodeString(encoded)
	defer clear(ciphertext)
	return err == nil && base64.StdEncoding.EncodeToString(ciphertext) == encoded
}

func (r *Repository) checkCredentials(state bundle) (*secrets.Vault, error) {
	vault, err := r.vaultLocked(secrets.Background)
	if err != nil {
		return nil, err
	}
	if err := checkCredentialsWithVault(state, vault); err != nil {
		return nil, err
	}
	return vault, nil
}

func checkCredentialsWithVault(state bundle, vault *secrets.Vault) error {
	for provider, envelope := range state.Credentials {
		if _, err := decodeCredential(vault, provider, envelope); err != nil {
			return err
		}
	}
	return nil
}

func decodeCredential(vault *secrets.Vault, provider string, envelope []byte) (providers.Credential, error) {
	plain, err := vault.Unprotect("credential:"+provider, envelope)
	if err != nil {
		return providers.Credential{}, errConfigurationUnavailable
	}
	defer clear(plain)
	credential, err := providers.ParseCredential(provider, plain)
	if err != nil {
		return providers.Credential{}, errConfigurationInvalid
	}
	return credential, nil
}

// Byte buffers are cleared best-effort. Go strings and garbage collection do
// not provide a guarantee of memory erasure for parsed credential values.
