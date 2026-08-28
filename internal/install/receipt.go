package install

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/configuration"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/hostfile"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/secrets"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

type backupReference struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
}
type receipt struct {
	AgentID       string           `json:"agentId"`
	ConfigPath    string           `json:"configPath"`
	TargetPath    string           `json:"targetPath"`
	Executable    string           `json:"executable"`
	PackageRoot   string           `json:"packageRoot"`
	DataDirectory string           `json:"dataDirectory"`
	Renderer      string           `json:"renderer"`
	Backup        *backupReference `json:"backup"`
	Entries       []ownedEntry     `json:"entries"`
	ShimText      string           `json:"shimText"`
	State         string           `json:"state"`
}

// Committed receipts cannot themselves hold transitions. A pending record has
// exactly one desired receipt and at most one previous committed receipt.
type transition struct {
	BeforeDigest       string   `json:"beforeDigest"`
	AfterDigest        string   `json:"afterDigest"`
	BeforeAccessDigest string   `json:"beforeAccessDigest"`
	AfterAccessDigests []string `json:"afterAccessDigests"`
	Desired            *receipt `json:"desired"`
	Previous           *receipt `json:"previous"`
}
type receiptRecord struct {
	SchemaVersion int         `json:"schemaVersion"`
	AgentID       string      `json:"agentId"`
	State         string      `json:"state"`
	Receipt       *receipt    `json:"receipt"`
	Pending       *transition `json:"pending"`
}

func contentDigest(exists bool, data []byte) string {
	h := sha256.New()
	h.Write([]byte("agent-task-notify/install/content/v1\x00"))
	if exists {
		h.Write([]byte{1})
		h.Write(data)
	} else {
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func receiptPath(r *configuration.Repository, agent string) string {
	return filepath.Join(r.Directory(), "receipts", agent+".json")
}

func receiptFrom(options Options, target, directory string, entries []ownedEntry, replacement []byte) *receipt {
	r := &receipt{AgentID: options.AgentID, ConfigPath: options.ConfigPath, TargetPath: target, Executable: options.Executable, PackageRoot: options.PackageRoot, DataDirectory: directory, Renderer: options.CommandShell, Entries: entries, State: "active"}
	if options.AgentID == "opencode" {
		r.Renderer = "direct"
		r.ShimText = string(replacement)
	}
	return r
}

func validReceipt(r *receipt, directory, agent string) bool {
	if r == nil || r.AgentID != agent || !automaticAgent(agent) || r.DataDirectory != directory || !cleanAbsolute(r.ConfigPath) || (r.State != "active" && r.State != "inactive") {
		return false
	}
	_, target, err := resolveTarget(agent, r.ConfigPath)
	if err != nil || target != r.TargetPath || !cleanAbsolute(r.PackageRoot) || !cleanAbsolute(r.Executable) || !within(r.Executable, r.PackageRoot) || r.Executable == r.PackageRoot || within(target, directory) || within(target, r.PackageRoot) {
		return false
	}
	if r.Backup == nil || !cleanAbsolute(r.Backup.Path) || filepath.Dir(r.Backup.Path) != filepath.Join(directory, "backups") {
		return false
	}
	name := filepath.Base(r.Backup.Path)
	if len(name) != 37 || name[32:] != ".json" || !validHex(name[:32], 32) {
		return false
	}
	if agent == "opencode" {
		shim, err := renderShim(r.Executable, r.PackageRoot, directory)
		return err == nil && r.Renderer == "direct" && len(r.Entries) == 0 && r.ShimText == string(shim)
	}
	command, err := renderCommand(r.Renderer, r.Executable, agent, directory)
	if err != nil || r.ShimText != "" {
		return false
	}
	want := ownedEntries(agent, command)
	if len(r.Entries) != len(want) {
		return false
	}
	for i, entry := range r.Entries {
		if entry.Event != want[i].Event || !equalJSON(entry.Value, want[i].Value) {
			return false
		}
	}
	return true
}

func validRecord(record *receiptRecord, directory, agent string) bool {
	if record.SchemaVersion != 1 || record.AgentID != agent {
		return false
	}
	if record.State == "active" || record.State == "inactive" {
		return record.Pending == nil && validReceipt(record.Receipt, directory, agent) && record.Receipt.State == record.State
	}
	p := record.Pending
	if record.State != "pending" || record.Receipt != nil || p == nil || !validReceipt(p.Desired, directory, agent) || !validHex(p.BeforeDigest, 64) || !validHex(p.AfterDigest, 64) || !validHex(p.BeforeAccessDigest, 64) || len(p.AfterAccessDigests) < 1 || len(p.AfterAccessDigests) > 2 {
		return false
	}
	for _, d := range p.AfterAccessDigests {
		if !validHex(d, 64) {
			return false
		}
	}
	if len(p.AfterAccessDigests) == 2 && p.AfterAccessDigests[0] == p.AfterAccessDigests[1] {
		return false
	}
	if p.Previous != nil && (!validReceipt(p.Previous, directory, agent) || p.Previous.TargetPath != p.Desired.TargetPath || *p.Previous.Backup != *p.Desired.Backup) {
		return false
	}
	return true
}

func readRecord(r *configuration.Repository, agent string) (*receiptRecord, []byte, error) {
	if !automaticAgent(agent) || r == nil {
		return nil, nil, ErrInvalid
	}
	if _, err := os.Lstat(r.Directory()); os.IsNotExist(err) {
		return nil, nil, nil
	}
	if store.CheckPrivateDirectory(r.Directory()) != nil {
		return nil, nil, ErrUnavailable
	}
	dir := filepath.Join(r.Directory(), "receipts")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil, nil, nil
	}
	data, err := store.ReadPrivate(receiptPath(r, agent), strictjson.MaxBytes)
	if err == store.ErrNotFound {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	if _, err := strictjson.Object(data); err != nil {
		return nil, nil, ErrInvalid
	}
	var record receiptRecord
	if json.Unmarshal(data, &record) != nil {
		return nil, nil, ErrInvalid
	}
	// A strict roundtrip also rejects unknown/missing/case-aliased fields at
	// every typed level. Raw owned values are separately recomputed below.
	encoded, err := json.Marshal(record)
	if err != nil || !equalJSON(data, encoded) || !validRecord(&record, r.Directory(), agent) {
		return nil, nil, ErrInvalid
	}
	return &record, data, nil
}

func recordTarget(r *receiptRecord) *receipt {
	if r == nil {
		return nil
	}
	if r.State == "pending" {
		return r.Pending.Desired
	}
	return r.Receipt
}

func resolveRecord(record *receiptRecord, before hostfile.Snapshot) (*receipt, error) {
	if record == nil {
		return nil, nil
	}
	if record.State != "pending" {
		return record.Receipt, nil
	}
	p := record.Pending
	access, err := before.AccessDigest()
	if err != nil {
		return nil, ErrConflict
	}
	digest := contentDigest(before.Exists, before.Data)
	if digest == p.AfterDigest && slices.Contains(p.AfterAccessDigests, access) {
		return p.Desired, nil
	}
	if digest == p.BeforeDigest && access == p.BeforeAccessDigest {
		return p.Previous, nil
	}
	return nil, ErrConflict
}

func verifyBackup(vault *secrets.Vault, agent string, backup *backupReference) error {
	if backup == nil {
		return ErrInvalid
	}
	envelope, err := store.ReadPrivate(backup.Path, 6<<20)
	if err != nil {
		return ErrUnavailable
	}
	defer clear(envelope)
	plain, err := vault.Unprotect("backup:"+agent, envelope)
	if err != nil {
		return ErrUnavailable
	}
	defer clear(plain)
	if !backup.Existed && len(plain) != 0 {
		return ErrInvalid
	}
	return nil
}

// Planning binds the encrypted bytes without opening or authorizing a Vault.
func readBackup(r *receipt) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if r.Backup == nil {
		return nil, ErrInvalid
	}
	data, err := store.ReadPrivate(r.Backup.Path, 6<<20)
	if err != nil || len(data) == 0 {
		return nil, ErrUnavailable
	}
	return data, nil
}

func protectBackup(ctx context.Context, r *configuration.Repository, vault *secrets.Vault, agent string, existed bool, original []byte, write func(string, []byte) error) (*backupReference, error) {
	if ctx == nil || ctx.Err() != nil || !automaticAgent(agent) || (!existed && len(original) != 0) {
		return nil, ErrInvalid
	}
	envelope, err := vault.Protect("backup:"+agent, original)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer clear(envelope)
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, ErrUnavailable
	}
	backup := &backupReference{filepath.Join(r.Directory(), "backups", hex.EncodeToString(random[:])+".json"), existed}
	if ctx.Err() != nil || write(backup.Path, envelope) != nil {
		return nil, ErrUnavailable
	}
	actual, err := store.ReadPrivate(backup.Path, 6<<20)
	if err != nil || !bytes.Equal(actual, envelope) {
		return nil, ErrUnavailable
	}
	defer clear(actual)
	if verifyBackup(vault, agent, backup) != nil {
		return nil, ErrUnavailable
	}
	return backup, nil
}

func marshalRecord(r *configuration.Repository, record *receiptRecord) ([]byte, error) {
	if !validRecord(record, r.Directory(), record.AgentID) {
		return nil, ErrInvalid
	}
	data, err := json.Marshal(record)
	if err != nil || len(data) > strictjson.MaxBytes {
		return nil, ErrInvalid
	}
	return data, nil
}

func writeRecord(ctx context.Context, r *configuration.Repository, record *receiptRecord, write func(string, []byte) error) error {
	data, err := marshalRecord(r, record)
	if err != nil {
		return err
	}
	if ctx.Err() != nil || write(receiptPath(r, record.AgentID), data) != nil {
		return ErrUnavailable
	}
	actual, err := store.ReadPrivate(receiptPath(r, record.AgentID), strictjson.MaxBytes)
	if err != nil || !bytes.Equal(actual, data) {
		return ErrUnavailable
	}
	return nil
}
