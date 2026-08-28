package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"testing"
)

// This fixed inventory is independent of the decoder's inventory helper.
func darwinFixtureEntries(t *testing.T) []entry {
	t.Helper()
	// A minimal, non-executed Mach-O header: 64-bit AMD64 executable, no loads.
	binary := []byte{0xcf, 0xfa, 0xed, 0xfe, 7, 0, 0, 1, 3, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	entries := []entry{
		{"INSTALL.md", []byte("install\n"), 0644},
		{"INSTALL.zh-CN.md", []byte("安装\n"), 0644},
		{"LICENSE", []byte("license\n"), 0644},
		{"THIRD_PARTY_NOTICES.md", []byte("notices\n"), 0644},
		{"UNSIGNED-CANDIDATE.txt", []byte("UNSIGNED CANDIDATE — experimental CI test artifact only.\nNot signed or notarized for end-user distribution. Stop if macOS blocks execution; do not bypass Gatekeeper or remove quarantine.\nRead packaged INSTALL.md or INSTALL.zh-CN.md for the explicit experimental setup and evidence boundaries.\n"), 0644},
		{"agent-task-notify", binary, 0755},
		{"integrations/opencode/agent-task-notify.mjs", []byte("export {}\n"), 0644},
		{"integrations/opencode/bridge.mjs", []byte("export {}\n"), 0644},
		{"manifest.json", []byte("{}\n"), 0644},
		{"skills/agent-task-notify/SKILL.md", []byte("skill\n"), 0644},
		{"skills/agent-task-notify/agents/openai.yaml", []byte("name: test\n"), 0644},
		{"workbuddy/.workbuddy-plugin/plugin.json", []byte("{}\n"), 0644},
		{"workbuddy/hooks/hooks.json", []byte("{}\n"), 0644},
		{"workbuddy/hooks/launch.sh", []byte("#!/bin/sh\n"), 0755},
		{"workbuddy/runtime/agent-task-notify", binary, 0755},
	}
	var files []string
	for _, e := range entries {
		files = append(files, e.name)
	}
	sum := sha256.Sum256(binary)
	m, err := json.Marshal(map[string]any{"schemaVersion": 1, "version": "0.2.0-rc.1", "platform": "darwin-amd64", "binarySHA256": hex.EncodeToString(sum[:]), "files": files})
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		if entries[i].name == "manifest.json" {
			entries[i].data = m
		}
	}
	return entries
}

func writeFixtureTar(t *testing.T, w io.Writer) {
	t.Helper()
	tw := tar.NewWriter(w)
	for _, e := range darwinFixtureEntries(t) {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: int64(e.mode), Size: int64(len(e.data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
}

func gzipFixture(t *testing.T, write func(io.Writer)) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	write(gz)
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// tar.Writer deliberately disallows explicit local metadata headers. Build a
// checksummed empty metadata record; tar.Next consumes these internally.
func emptyMetadataHeader(kind byte) []byte {
	h := make([]byte, 512)
	copy(h, "metadata")
	copy(h[100:108], "0000644\x00")
	copy(h[108:116], "0000000\x00")
	copy(h[116:124], "0000000\x00")
	copy(h[124:136], "00000000000\x00")
	copy(h[136:148], "00000000000\x00")
	copy(h[148:156], "        ")
	h[156] = kind
	copy(h[257:265], "ustar\x0000")
	var sum int
	for _, b := range h {
		sum += int(b)
	}
	copy(h[148:156], fmt.Sprintf("%06o\x00 ", sum))
	return h
}

func TestDecodeDarwinArchiveExpandedMetadataBudget(t *testing.T) {
	// Removing the pre-tar aggregate budget accepts these otherwise canonical
	// inventories after consuming more than 221 MiB of hidden metadata.
	for _, kind := range []byte{tar.TypeXHeader, tar.TypeGNULongName} {
		t.Run(string(kind), func(t *testing.T) {
			data := gzipFixture(t, func(w io.Writer) {
				block := bytes.Repeat(emptyMetadataHeader(kind), 128)
				const headers = 221*1024*1024/512 + 1
				for left := headers; left > 0; {
					n := min(left, 128)
					if _, err := w.Write(block[:n*512]); err != nil {
						t.Fatal(err)
					}
					left -= n
				}
				writeFixtureTar(t, w)
			})
			if _, err := decodeArchive(data, "darwin-amd64"); err == nil {
				t.Fatal("accepted archive with over 221 MiB of expanded metadata")
			}
		})
	}
}

func TestDecodeDarwinArchiveCanonicalAndGzipBoundaries(t *testing.T) {
	var raw bytes.Buffer
	writeFixtureTar(t, &raw)
	budget := int64(raw.Len())
	good := gzipFixture(t, func(w io.Writer) { writeFixtureTar(t, w) })
	got, err := decodeArchive(good, "darwin-amd64")
	if err != nil || !reflect.DeepEqual(got, darwinFixtureEntries(t)) {
		t.Fatalf("canonical decoder round trip: %v", err)
	}
	if err := verifyContents(got, "darwin-amd64"); err != nil {
		t.Fatalf("canonical content/architecture check: %v", err)
	}
	for _, limit := range []int64{budget, budget + 1} {
		if _, err := decodeArchiveWithExpandedLimit(good, "darwin-amd64", limit); err != nil {
			t.Fatalf("valid archive at/inside expanded boundary: %v", err)
		}
	}
	for _, limit := range []int64{0, budget - 1, budget - 512, budget - 1024} {
		if _, err := decodeArchiveWithExpandedLimit(good, "darwin-amd64", limit); err == nil {
			t.Fatalf("limit exhaustion accepted as EOF: limit=%d", limit)
		}
	}
	crc := bytes.Clone(good)
	crc[len(crc)-8] ^= 1
	trailing := gzipFixture(t, func(w io.Writer) {
		writeFixtureTar(t, w)
		_, _ = w.Write([]byte{0})
	})
	for name, data := range map[string][]byte{
		"crc":                      crc,
		"truncated-trailer":        good[:len(good)-1],
		"truncated-stream":         good[:len(good)/2],
		"tar-trailing-byte":        trailing,
		"compressed-trailing-byte": append(bytes.Clone(good), 0),
		"multistream":              append(bytes.Clone(good), good...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeArchive(data, "darwin-amd64"); err == nil {
				t.Fatal("accepted invalid gzip/tar boundary")
			}
			if _, err := decodeArchiveWithExpandedLimit(data, "darwin-amd64", budget); err == nil {
				t.Fatal("accepted invalid gzip/tar at exact expanded boundary")
			}
		})
	}
}
