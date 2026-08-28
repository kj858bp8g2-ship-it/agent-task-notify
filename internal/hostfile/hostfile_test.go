package hostfile

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(hostDirectory(t), "host.json")
	writeFixture(t, path, []byte(`{"other":1}`))
	return path
}

func TestAccessDigestsPredictNativeReplacementAndRemoval(t *testing.T) {
	for _, present := range []bool{false, true} {
		path := fixture(t)
		if !present {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		before := snapshot(t, path)
		initial, err := before.AccessDigest()
		if err != nil {
			t.Fatal(err)
		}
		candidates, err := before.ExpectedAccessDigests(true)
		if err != nil {
			t.Fatal(err)
		}
		if len(candidates) < 1 || len(candidates) > 2 {
			t.Fatal("unbounded access candidates")
		}
		if !present && len(candidates) != 1 {
			t.Fatal("new access must have one exact prediction")
		}
		for _, value := range append(candidates, initial) {
			if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(value) {
				t.Fatal("access digest leaked metadata or has invalid format")
			}
		}
		if len(candidates) == 2 && candidates[0] == candidates[1] {
			t.Fatal("duplicate candidates")
		}
		entriesBefore, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		before.Exists = !before.Exists
		before.Data = []byte("untrusted view")
		unchanged, err := before.AccessDigest()
		if err != nil || unchanged != initial {
			t.Fatal("public snapshot fields changed access authority")
		}
		again, err := before.ExpectedAccessDigests(true)
		if err != nil || !slices.Equal(again, candidates) {
			t.Fatal("expected access changed")
		}
		entriesAfter, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entriesBefore) != len(entriesAfter) {
			t.Fatal("digest methods wrote files")
		}
		if err := Replace(path, before, []byte("replacement")); err != nil {
			t.Fatal(err)
		}
		after := snapshot(t, path)
		actual, err := after.AccessDigest()
		if err != nil || !slices.Contains(candidates, actual) {
			t.Fatal("native replacement access not predicted")
		}
		absent, err := after.ExpectedAccessDigests(false)
		if err != nil || len(absent) != 1 {
			t.Fatal("missing absence prediction")
		}
		if err := Remove(path, after); err != nil {
			t.Fatal(err)
		}
		removed, err := snapshot(t, path).AccessDigest()
		if err != nil || removed != absent[0] {
			t.Fatal("native removal access not predicted")
		}
		if actual == removed {
			t.Fatal("existence missing from digest")
		}
	}
}

func TestAccessDigestRejectsForgeryAndDistinguishesChangedMetadata(t *testing.T) {
	for _, value := range []Snapshot{{}, {Data: []byte("x"), Exists: true}} {
		if _, err := value.AccessDigest(); !errors.Is(err, ErrUnsafe) {
			t.Fatal("forged access digest")
		}
		if _, err := value.ExpectedAccessDigests(true); !errors.Is(err, ErrUnsafe) {
			t.Fatal("forged expected access")
		}
		if _, err := value.ExpectedAccessDigests(false); !errors.Is(err, ErrUnsafe) {
			t.Fatal("forged absence access")
		}
	}
	path := fixture(t)
	before := snapshot(t, path)
	digest, err := before.AccessDigest()
	if err != nil {
		t.Fatal(err)
	}
	changeAccess(t, path)
	after, err := snapshot(t, path).AccessDigest()
	if err != nil || after == digest {
		t.Fatal("changed access has same digest")
	}
	if old, err := before.AccessDigest(); err != nil || old != digest {
		t.Fatal("snapshot digest depends on later filesystem state")
	}
}

func snapshot(t *testing.T, path string) Snapshot {
	t.Helper()
	before, err := Read(path, 4096)
	if err != nil {
		t.Fatal(err)
	}
	return before
}

func assertData(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatal("unexpected file contents")
	}
}

// Catches stale-content replacement and removal, not just stale identities.
func TestExternalEditIsNotOverwritten(t *testing.T) {
	for _, remove := range []bool{false, true} {
		path := fixture(t)
		before := snapshot(t, path)
		if err := os.WriteFile(path, []byte(`{"external":2}`), 0600); err != nil {
			t.Fatal(err)
		}
		var err error
		if remove {
			err = Remove(path, before)
		} else {
			err = Replace(path, before, []byte(`{"ours":3}`))
		}
		if err == nil {
			t.Fatal("overwrote external edit")
		}
		assertData(t, path, `{"external":2}`)
	}
}

func TestExistenceAndIdentityConflicts(t *testing.T) {
	for _, change := range []string{"appeared", "disappeared", "different-identity", "metadata"} {
		t.Run(change, func(t *testing.T) {
			path := fixture(t)
			if change == "appeared" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshot(t, path)
			switch change {
			case "appeared":
				writeFixture(t, path, []byte("external"))
			case "disappeared":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "different-identity":
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, path, before.Data)
			case "metadata":
				changeAccess(t, path)
			}
			if err := Replace(path, before, []byte("ours")); err == nil {
				t.Fatal("conflicting replacement accepted")
			}
			if err := Remove(path, before); err == nil {
				t.Fatal("conflicting removal accepted")
			}
			if change == "disappeared" {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("missing target recreated")
				}
			} else if change == "appeared" {
				assertData(t, path, "external")
			} else {
				assertData(t, path, `{"other":1}`)
			}
		})
	}
}

func TestSnapshotCannotBeForgedOrRebound(t *testing.T) {
	path := fixture(t)
	other := fixture(t)
	before := snapshot(t, path)
	if Replace(other, before, []byte("bad")) == nil || Remove(other, before) == nil {
		t.Fatal("snapshot rebound")
	}
	if Replace(path, Snapshot{Data: before.Data, Exists: true}, []byte("bad")) == nil {
		t.Fatal("forged snapshot accepted")
	}
	before.Data[0] = '!'
	before.Exists = false
	if err := Replace(path, before, []byte("good")); err != nil {
		t.Fatal("public fields changed opaque authority", err)
	}
	assertData(t, path, "good")
	assertData(t, other, `{"other":1}`)
}

func TestBoundedSideEffectFreeRead(t *testing.T) {
	path := fixture(t)
	access := accessListing(t, path)
	for _, limit := range []int64{-1, 0, 1, math.MaxInt64} {
		if _, err := Read(path, limit); err == nil {
			t.Fatal("invalid bound accepted")
		}
	}
	before := snapshot(t, path)
	if !before.Exists || string(before.Data) != `{"other":1}` {
		t.Fatal("incorrect snapshot")
	}
	if accessListing(t, path) != access {
		t.Fatal("read changed access")
	}
	missing := filepath.Join(filepath.Dir(path), "missing")
	if snapshot(t, missing).Exists {
		t.Fatal("absent target reported present")
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatal("read created file")
	}
	if _, err := Read(filepath.Join(missing, "child"), 4096); err == nil {
		t.Fatal("missing parent accepted")
	}
	if _, err := Read(filepath.Dir(path), 4096); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err := Read("relative.json", 4096); err == nil {
		t.Fatal("relative path accepted")
	}
}

func TestReplacementPreservesAccessAndRemoveIsSingleFile(t *testing.T) {
	path := fixture(t)
	parent := accessListing(t, filepath.Dir(path))
	access := accessListing(t, path)
	if err := Replace(path, snapshot(t, path), []byte("new contents")); err != nil {
		t.Fatal(err)
	}
	assertData(t, path, "new contents")
	if accessListing(t, path) != access {
		t.Fatal("replacement changed access")
	}
	sibling := filepath.Join(filepath.Dir(path), "sibling")
	writeFixture(t, sibling, []byte("keep"))
	if err := Remove(path, snapshot(t, path)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("file not removed")
	}
	assertData(t, sibling, "keep")
	if accessListing(t, filepath.Dir(path)) != parent {
		t.Fatal("parent access changed")
	}
	if err := Remove(path, snapshot(t, path)); err == nil {
		t.Fatal("absent snapshot accepted for removal")
	}
}

func TestCreateMissingTargetIsPrivate(t *testing.T) {
	path := filepath.Join(hostDirectory(t), "new.json")
	if err := Replace(path, snapshot(t, path), []byte("new")); err != nil {
		t.Fatal(err)
	}
	assertData(t, path, "new")
	assertOwnerOnly(t, path)
}

func TestReadOnlyIsNeverBypassed(t *testing.T) {
	path := fixture(t)
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	before := snapshot(t, path)
	access := accessListing(t, path)
	if Replace(path, before, []byte("bad")) == nil || Remove(path, before) == nil {
		t.Fatal("read-only target modified")
	}
	assertData(t, path, `{"other":1}`)
	if accessListing(t, path) != access {
		t.Fatal("read-only permissions changed")
	}
}

func TestPreparedTemporaryIsEmptyWithFinalAccess(t *testing.T) {
	for _, present := range []bool{false, true} {
		path := fixture(t)
		before := snapshot(t, path)
		var source *os.File
		if present {
			var err error
			source, err = nativeOpen(path, true)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
		}
		temp, _, err := prepareTemporary(path, source, before.state.access)
		if err != nil {
			t.Fatal(err)
		}
		defer temp.Close()
		info, err := temp.Stat()
		if err != nil || info.Size() != 0 {
			t.Fatal("data written before security prepared")
		}
		if present {
			if accessListing(t, temp.Name()) != accessListing(t, path) {
				t.Fatal("temporary access differs before write")
			}
		} else {
			assertOwnerOnly(t, temp.Name())
		}
	}
}

func TestCleanupDoesNotRemoveAReusedTemporaryName(t *testing.T) {
	path := fixture(t)
	// Match production: identity is captured from an opened handle. Windows
	// path-only FileInfo may resolve its identity lazily after the name changes.
	opened, err := nativeOpen(path, false)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := opened.Stat()
	opened.Close()
	if err != nil {
		t.Fatal(err)
	}
	moved := path + ".held"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, []byte("not our temporary"))
	cleanupTemporary(path, identity)
	assertData(t, path, "not our temporary")
	cleanupTemporary(moved, identity)
	if _, err := os.Lstat(moved); !os.IsNotExist(err) {
		t.Fatal("exact owned temporary not removed")
	}
}

func TestCommittedVerificationFailureDoesNotClaimRollback(t *testing.T) {
	for _, change := range []string{"contents", "access", "identity", "missing"} {
		t.Run(change, func(t *testing.T) {
			path := fixture(t)
			before := snapshot(t, path)
			source, err := nativeOpen(path, true)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			temp, access, err := prepareTemporary(path, source, before.state.access)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := temp.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := temp.Write([]byte("committed")); err != nil {
				t.Fatal(err)
			}
			if err := temp.Sync(); err != nil {
				t.Fatal(err)
			}
			if err := temp.Close(); err != nil {
				t.Fatal(err)
			}
			if err := nativeReplace(temp.Name(), path, identity, access); err != nil {
				t.Fatal(err)
			}
			if err := verifyReplacement(path, []byte("committed"), access, identity); err != nil {
				t.Fatal("successful commit not verified")
			}
			switch change {
			case "contents":
				if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
					t.Fatal(err)
				}
			case "access":
				changeAccess(t, path)
			case "identity":
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, path, []byte("committed"))
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if err := verifyReplacement(path, []byte("committed"), access, identity); !errors.Is(err, ErrVerification) {
				t.Fatal("committed failure not distinguished")
			}
			if change == "contents" {
				assertData(t, path, "external")
			}
		})
	}
}
