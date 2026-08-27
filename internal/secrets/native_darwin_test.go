package secrets

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	keychain "github.com/keybase/go-keychain"
)

func cleanupSyntheticAccount(t *testing.T, id string) {
	t.Helper()
	t.Cleanup(func() {
		err := withInteraction(Foreground, func() error {
			item := keychain.NewItem()
			item.SetSecClass(keychain.SecClassGenericPassword)
			item.SetService("agenttasknotify.native.v1")
			item.SetAccount(id)
			item.SetSynchronizable(keychain.SynchronizableNo)
			err := keychain.DeleteItem(item)
			if err == keychain.ErrorItemNotFound {
				return nil
			}
			if err != nil {
				return ErrUnavailable
			}
			return nil
		})
		if err != nil {
			t.Error("synthetic account cleanup failed")
		}
	})
}

// Catches accidental creation in Background and failure to retain the same DEK.
func TestDarwinBackgroundOnlyReadsExistingSyntheticAccount(t *testing.T) {
	id := syntheticID(t)
	cleanupSyntheticAccount(t, id)
	_, err := Open(id, Background)
	requireSafeError(t, err)
	_, err = Open(id, Background)
	requireSafeError(t, err)
	v, err := Open(id, Foreground)
	if err != nil {
		t.Fatal("synthetic vault unavailable")
	}
	b, err := v.Protect("credential:bark", []byte("synthetic"))
	if err != nil {
		t.Fatal("protection failed")
	}
	w, err := Open(id, Background)
	if err != nil {
		t.Fatal("background vault unavailable")
	}
	got, err := w.Unprotect("credential:bark", b)
	if err != nil || string(got) != "synthetic" {
		t.Fatal("background changed key")
	}
}

func TestDarwinRejectsWrongKeyLength(t *testing.T) {
	id := syntheticID(t)
	cleanupSyntheticAccount(t, id)
	err := withInteraction(Foreground, func() error {
		item := keychain.NewItem()
		item.SetSecClass(keychain.SecClassGenericPassword)
		item.SetService("agenttasknotify.native.v1")
		item.SetAccount(id)
		item.SetSynchronizable(keychain.SynchronizableNo)
		item.SetData([]byte("synthetic-short"))
		if keychain.AddItem(item) != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		t.Fatal("synthetic fixture unavailable")
	}
	for _, mode := range []AccessMode{Foreground, Background} {
		_, err := Open(id, mode)
		requireSafeError(t, err)
	}
}

// This is deliberately not a normal local test: only a dedicated CI fixture
// may be locked. No login keychain is ever accepted, even under RUNNER_TEMP.
func TestDarwinLockedKeychainBackgroundDenial(t *testing.T) {
	if os.Getenv("CI") != "true" {
		t.Skip("CI-only dedicated locked-Keychain gate not executed")
	}
	fixture := os.Getenv("ATN_TEST_KEYCHAIN")
	root := os.Getenv("RUNNER_TEMP")
	if fixture == "" || root == "" {
		t.Fatal("dedicated CI Keychain fixture required; denial gate not executed")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal("unsafe CI fixture")
	}
	resolved, err := filepath.EvalSymlinks(fixture)
	if err != nil || !filepath.IsAbs(fixture) || !filepath.IsAbs(resolvedRoot) {
		t.Fatal("unsafe CI fixture")
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) || filepath.Base(resolved) != "synthetic.keychain-db" {
		t.Fatal("unsafe CI fixture")
	}
	fixtureDir := filepath.Dir(rel)
	if filepath.Base(fixtureDir) != fixtureDir || !strings.HasPrefix(fixtureDir, "atn-keychain.") || len(fixtureDir) <= len("atn-keychain.") {
		t.Fatal("unsafe CI fixture")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("unsafe CI fixture")
	}
	security := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "/usr/bin/security", args...).Run()
	}
	// Task 4 creates this disposable fixture using only this public synthetic password.
	const password = "atn-synthetic-ci-fixture-only"
	v, id := syntheticVault(t)
	sealed, err := v.Protect("credential:bark", []byte("synthetic locked fixture"))
	if err != nil {
		t.Fatal("fixture protection failed")
	}
	t.Cleanup(func() {
		if security("unlock-keychain", "-p", password, resolved) != nil {
			t.Error("fixture unlock failed")
		}
	})
	if security("lock-keychain", resolved) != nil {
		t.Fatal("fixture lock failed")
	}
	start := time.Now()
	done := make(chan error, 1)
	go func() { _, err := Open(id, Background); done <- err }()
	select {
	case err := <-done:
		requireSafeError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("background access exceeded denial deadline")
	}
	if time.Since(start) >= 5*time.Second {
		t.Fatal("background access exceeded denial deadline")
	}
	if security("unlock-keychain", "-p", password, resolved) != nil {
		t.Fatal("fixture unlock failed")
	}
	w, err := Open(id, Background)
	if err != nil {
		t.Fatal("original fixture inaccessible")
	}
	got, err := w.Unprotect("credential:bark", sealed)
	if err != nil || !bytes.Equal(got, []byte("synthetic locked fixture")) {
		t.Fatal("background replaced original key")
	}
}
