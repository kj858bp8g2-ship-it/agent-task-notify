package tests

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	encodingbinary "encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

type packageEntry struct {
	name string
	data []byte
	mode os.FileMode
	link bool
}

// Removing any safety gate, leaking extra source files, or running the source
// binary instead of the archived executable must fail these black-box checks.
func TestNativePackage(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "cmd", "package-native", "main.go")
	if _, err := os.Stat(source); err != nil {
		t.Fatal("native package build/verify command is missing")
	}
	tool := filepath.Join(t.TempDir(), "package-native"+exeSuffix())
	build := exec.Command("go", "build", "-trimpath", "-o", tool, "./cmd/package-native")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build package tool: %v: %s", err, output)
	}
	binary := nativeExecutable(t)
	platform := runtime.GOOS + "-" + runtime.GOARCH
	out := filepath.Join(t.TempDir(), "new-output")
	args := []string{"build", "--source-root", root, "--binary", binary, "--platform", platform, "--version", "0.2.0-rc.1", "--output", out}
	packageRun(t, tool, true, args...)
	name := "atn-native-0.2.0-rc.1-" + platform + ".tar.gz"
	if runtime.GOOS == "windows" {
		name = "atn-native-0.2.0-rc.1-" + platform + ".zip"
	}
	archive := filepath.Join(out, name)
	sums := filepath.Join(out, "SHA256SUMS")
	archiveData, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	checksum, err := os.ReadFile(sums)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archiveData)
	if string(checksum) != hex.EncodeToString(digest[:])+"  "+name+"\n" {
		t.Fatal("checksum is not exact archive basename/hash")
	}
	entries := packageRead(t, archiveData, runtime.GOOS == "windows")
	binaryName := "agent-task-notify" + exeSuffix()
	want := []string{binaryName, "LICENSE", "THIRD_PARTY_NOTICES.md", "manifest.json", "INSTALL.md", "INSTALL.zh-CN.md", "skills/agent-task-notify/SKILL.md", "skills/agent-task-notify/agents/openai.yaml", "integrations/opencode/agent-task-notify.mjs", "integrations/opencode/bridge.mjs", "workbuddy/.workbuddy-plugin/plugin.json", "workbuddy/hooks/hooks.json", "workbuddy/hooks/launch.sh", "workbuddy/runtime/" + binaryName}
	if runtime.GOOS == "darwin" {
		want = append(want, "UNSIGNED-CANDIDATE.txt")
	}
	sort.Strings(want)
	var names []string
	for _, e := range entries {
		names = append(names, e.name)
		if e.link {
			t.Fatal("link in package")
		}
		expected := os.FileMode(0644)
		if e.name == binaryName || e.name == "workbuddy/runtime/"+binaryName || e.name == "workbuddy/hooks/launch.sh" {
			expected = 0755
		}
		if e.mode.Perm() != expected {
			t.Fatalf("wrong archive mode %s: %o", e.name, e.mode)
		}
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("nonexact package inventory: %v", names)
	}
	var manifest struct {
		SchemaVersion int      `json:"schemaVersion"`
		Version       string   `json:"version"`
		Platform      string   `json:"platform"`
		BinarySHA256  string   `json:"binarySHA256"`
		Files         []string `json:"files"`
	}
	for _, e := range entries {
		switch e.name {
		case "manifest.json":
			if json.Unmarshal(e.data, &manifest) != nil {
				t.Fatal("invalid manifest")
			}
		case "workbuddy/hooks/launch.sh":
			if bytes.Contains(e.data, []byte("\r")) {
				t.Fatal("launcher must be LF")
			}
		case "UNSIGNED-CANDIDATE.txt":
			if !bytes.Contains(e.data, []byte("UNSIGNED")) {
				t.Fatal("unsigned marker missing")
			}
		}
	}
	binaryData, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	binHash := sha256.Sum256(binaryData)
	if manifest.SchemaVersion != 1 || manifest.Version != "0.2.0-rc.1" || manifest.Platform != platform || manifest.BinarySHA256 != hex.EncodeToString(binHash[:]) || !reflect.DeepEqual(manifest.Files, want) {
		t.Fatal("manifest mismatch")
	}
	extract := filepath.Join(t.TempDir(), "中文 空格 candidate")
	verifyArgs := []string{"verify", "--archive", archive, "--checksums", sums, "--platform", platform, "--version", "0.2.0-rc.1", "--extract-to", extract}
	output := packageRun(t, tool, true, verifyArgs...)
	if !strings.Contains(output, "verified agent-task-notify 0.2.0-rc.1 "+platform) || !strings.Contains(output, "doctor and six dry previews passed") {
		t.Fatal("missing actual execution evidence")
	}
	for _, e := range entries {
		got, err := os.ReadFile(filepath.Join(extract, filepath.FromSlash(e.name)))
		if err != nil || !bytes.Equal(got, e.data) {
			t.Fatal("extraction content differs")
		}
	}
	// Exact copied binary bytes plus six dry Agent lookups exercise embedded
	// data. Separately require the actual archive binary to retain both embedded
	// resources, with no mutable resource file in its fixed package inventory.
	for _, resource := range []string{"config/defaults.json", "assets/agent-icons.json"} {
		embedded, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(resource)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(binaryData, embedded) {
			t.Fatal("archive binary lost embedded resource", resource)
		}
	}
	packageRun(t, tool, false, verifyArgs...)
	packageRun(t, tool, false, args...)
	// Reject an empty preexisting directory too, and do not change its contents.
	t.Run("existing-empty", func(t *testing.T) {
		v := append([]string(nil), verifyArgs...)
		v[len(v)-1] = t.TempDir()
		packageRun(t, tool, false, v...)
		children, err := os.ReadDir(v[len(v)-1])
		if err != nil || len(children) != 0 {
			t.Fatal("preexisting directory modified")
		}
	})
	t.Run("wrong-name", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "other.zip")
		if os.WriteFile(path, archiveData, 0600) != nil {
			t.Fatal("fixture")
		}
		v := append([]string(nil), verifyArgs...)
		v[2] = path
		v[len(v)-1] = filepath.Join(t.TempDir(), "new")
		packageRun(t, tool, false, v...)
		assertPackageAbsent(t, v[len(v)-1])
	})
	for _, c := range []struct{ name, flag, value string }{{"version", "--version", "0.2.0-rc.1/other"}, {"platform", "--platform", "linux-amd64"}, {"wrong-arch", "--platform", map[string]string{"windows": "darwin-amd64", "darwin": "windows-amd64"}[runtime.GOOS]}} {
		t.Run(c.name, func(t *testing.T) {
			bad := append([]string(nil), args...)
			for i := range bad {
				if bad[i] == c.flag {
					bad[i+1] = c.value
				}
				if bad[i] == "--output" {
					bad[i+1] = filepath.Join(t.TempDir(), "output")
				}
			}
			packageRun(t, tool, false, bad...)
		})
	}
	t.Run("checksum", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "SHA256SUMS")
		if os.WriteFile(bad, []byte(strings.Repeat("0", 64)+"  "+name+"\n"), 0600) != nil {
			t.Fatal("fixture")
		}
		v := append([]string(nil), verifyArgs...)
		v[4] = bad
		v[len(v)-1] = filepath.Join(t.TempDir(), "extracted")
		packageRun(t, tool, false, v...)
		assertPackageAbsent(t, v[len(v)-1])
	})
	mutations := []struct {
		name   string
		change func([]packageEntry) []packageEntry
	}{
		{"traversal", func(e []packageEntry) []packageEntry {
			e[0].name = "../outside"
			return e
		}},
		{"absolute", func(e []packageEntry) []packageEntry {
			e[0].name = "/outside"
			return e
		}},
		{"backslash", func(e []packageEntry) []packageEntry {
			e[0].name = "workbuddy\\outside"
			return e
		}},
		{"duplicate", func(e []packageEntry) []packageEntry { e[1] = e[0]; return e }},
		{"extra", func(e []packageEntry) []packageEntry {
			e[0].name = "credentials.json"
			return e
		}},
		{"missing", func(e []packageEntry) []packageEntry { return e[1:] }},
		{"symlink", func(e []packageEntry) []packageEntry { e[0].link = true; return e }},
		{"mode", func(e []packageEntry) []packageEntry { e[0].mode = 0777; return e }},
		{"executable-mode", func(e []packageEntry) []packageEntry {
			for i := range e {
				if e[i].name == binaryName {
					e[i].mode = 0644
				}
			}
			return e
		}},
		{"oversize-binary", func(e []packageEntry) []packageEntry {
			for i := range e {
				if e[i].name == binaryName {
					e[i].data = make([]byte, 100*1024*1024+1)
				}
			}
			return e
		}},
		{"invalid-utf8", func(e []packageEntry) []packageEntry { e[0].data = []byte{0xff}; return e }},
		{"manifest-duplicate", func(e []packageEntry) []packageEntry {
			for i := range e {
				if e[i].name == "manifest.json" {
					e[i].data = bytes.Replace(e[i].data, []byte(`"schemaVersion": 1`), []byte(`"schemaVersion": 1, "schemaVersion": 1`), 1)
				}
			}
			return e
		}},
		{"architecture-with-valid-hashes", func(e []packageEntry) []packageEntry {
			modified := append([]byte(nil), binaryData...)
			if runtime.GOOS == "windows" {
				offset := encodingbinary.LittleEndian.Uint32(modified[0x3c:])
				encodingbinary.LittleEndian.PutUint16(modified[offset+4:], 0xaa64)
			} else {
				cpu := uint32(0x01000007)
				if runtime.GOARCH == "amd64" {
					cpu = 0x0100000c
				}
				encodingbinary.LittleEndian.PutUint32(modified[4:], cpu)
			}
			changedHash := sha256.Sum256(modified)
			for i := range e {
				if e[i].name == binaryName || e[i].name == "workbuddy/runtime/"+binaryName {
					e[i].data = modified
				}
				if e[i].name == "manifest.json" {
					e[i].data = bytes.Replace(e[i].data, []byte(manifest.BinarySHA256), []byte(hex.EncodeToString(changedHash[:])), 1)
				}
			}
			return e
		}},
		{"oversize-text", func(e []packageEntry) []packageEntry {
			for i := range e {
				if e[i].name == "INSTALL.md" {
					e[i].data = bytes.Repeat([]byte("x"), 1024*1024+1)
				}
			}
			return e
		}},
		{"manifest", func(e []packageEntry) []packageEntry {
			for i := range e {
				if e[i].name == "manifest.json" {
					e[i].data = []byte(`{"schemaVersion":1,"version":"0.1.0"}`)
				}
			}
			return e
		}},
		{"binary-hash", func(e []packageEntry) []packageEntry {
			for i := range e {
				if e[i].name == binaryName {
					e[i].data = []byte("not executable")
				}
			}
			return e
		}},
		{"workbuddy-binary", func(e []packageEntry) []packageEntry {
			for i := range e {
				if e[i].name == "workbuddy/runtime/"+binaryName {
					e[i].data = []byte("not same binary")
				}
			}
			return e
		}},
	}
	for _, c := range mutations {
		t.Run(c.name, func(t *testing.T) {
			copyEntries := append([]packageEntry(nil), entries...)
			data := packageWrite(t, c.change(copyEntries), runtime.GOOS == "windows")
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			sum := sha256.Sum256(data)
			sumPath := filepath.Join(dir, "SHA256SUMS")
			if os.WriteFile(path, data, 0600) != nil || os.WriteFile(sumPath, []byte(fmt.Sprintf("%x  %s\n", sum, name)), 0600) != nil {
				t.Fatal("fixture")
			}
			dest := filepath.Join(dir, "extract")
			packageRun(t, tool, false, "verify", "--archive", path, "--checksums", sumPath, "--platform", platform, "--version", "0.2.0-rc.1", "--extract-to", dest)
			assertPackageAbsent(t, dest)
		})
	}
	t.Run("workbuddy", func(t *testing.T) {
		bash, err := exec.LookPath("bash")
		if err != nil {
			t.Fatal(err)
		}
		hookData, err := os.ReadFile(filepath.Join(extract, "workbuddy/hooks/hooks.json"))
		var hooks struct {
			Hooks map[string][]struct {
				Hooks []struct{ Type, Command string }
			}
		}
		if err != nil || json.Unmarshal(hookData, &hooks) != nil || len(hooks.Hooks) != 2 {
			t.Fatal("native WorkBuddy hook configuration")
		}
		commands := map[string]string{}
		for _, event := range []string{"UserPromptSubmit", "Stop"} {
			groups := hooks.Hooks[event]
			if len(groups) != 1 || len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Type != "command" {
				t.Fatal("native WorkBuddy hook event")
			}
			commands[event] = groups[0].Hooks[0].Command
		}
		dir := t.TempDir()
		// Execute the command from the packaged hook configuration, not a
		// separately reconstructed command that could hide bad quoting/paths.
		cmd := exec.Command(bash, "-c", commands["UserPromptSubmit"])
		cmd.Dir = dir
		cmd.Env = append(withoutEnvironmentNames(os.Environ(), "CODEBUDDY_PLUGIN_ROOT", "CLAUDE_PLUGIN_ROOT", "ATN_DATA_DIRECTORY"), "CODEBUDDY_PLUGIN_ROOT="+filepath.ToSlash(filepath.Join(extract, "workbuddy")), "ATN_DATA_DIRECTORY="+filepath.Join(dir, "unused-data"))
		cmd.Stdin = strings.NewReader("[")
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "{\"continue\":true}\n" {
			t.Fatalf("native WorkBuddy neutral execution: %v %s", err, output)
		}
		// Invalid input increments only isolated diagnostics, proving the actual
		// archived executable (not just the wrapper's neutral fallback) ran.
		diagnostic, err := os.ReadFile(filepath.Join(dir, "unused-data", "input-diagnostics.json"))
		var observed struct {
			Count int `json:"count"`
		}
		if err != nil || json.Unmarshal(diagnostic, &observed) != nil || observed.Count != 1 {
			t.Fatal("WorkBuddy did not pass stdin to archived native binary")
		}
		for _, c := range []struct {
			name, primary, fallback string
			count                   int
		}{
			{"fallback", "", filepath.ToSlash(filepath.Join(extract, "workbuddy")), 2},
			{"primary-wins", filepath.ToSlash(filepath.Join(dir, "missing")), filepath.ToSlash(filepath.Join(extract, "workbuddy")), 2},
			{"no-root", "", "", 2},
			{"relative-root", "relative", "", 2},
		} {
			t.Run(c.name, func(t *testing.T) {
				cmd := exec.Command(bash, filepath.ToSlash(filepath.Join(extract, "workbuddy/hooks/launch.sh")))
				if c.name == "fallback" {
					cmd = exec.Command(bash, "-c", commands["Stop"])
				}
				cmd.Dir = dir
				cmd.Env = append(withoutEnvironmentNames(os.Environ(), "CODEBUDDY_PLUGIN_ROOT", "CLAUDE_PLUGIN_ROOT", "ATN_DATA_DIRECTORY"), "CODEBUDDY_PLUGIN_ROOT="+c.primary, "CLAUDE_PLUGIN_ROOT="+c.fallback, "ATN_DATA_DIRECTORY="+filepath.Join(dir, "unused-data"))
				cmd.Stdin = strings.NewReader("[")
				out, err := cmd.CombinedOutput()
				if err != nil || string(out) != "{\"continue\":true}\n" {
					t.Fatal("WorkBuddy failure response")
				}
				data, err := os.ReadFile(filepath.Join(dir, "unused-data", "input-diagnostics.json"))
				if err != nil || json.Unmarshal(data, &observed) != nil || observed.Count != c.count {
					t.Fatal("WorkBuddy root selection or fallback changed")
				}
			})
		}
	})
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
func assertPackageAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("invalid archive created extraction directory")
	}
}
func packageRun(t *testing.T, tool string, success bool, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, args...)
	output, err := cmd.CombinedOutput()
	if (err == nil) != success {
		t.Fatalf("package %s success=%v: %v %s", args[0], success, err, output)
	}
	if !success {
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 || string(output) != "native package rejected\n" {
			t.Fatalf("package rejection was not an explicit bounded refusal: %v %s", err, output)
		}
	}
	return string(output)
}
func packageRead(t *testing.T, data []byte, isZIP bool) []packageEntry {
	t.Helper()
	var entries []packageEntry
	if isZIP {
		r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range r.File {
			reader, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(reader)
			reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			entries = append(entries, packageEntry{f.Name, b, f.Mode(), f.Mode()&os.ModeSymlink != 0})
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		defer gz.Close()
		r := tar.NewReader(gz)
		for {
			h, err := r.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			entries = append(entries, packageEntry{h.Name, b, os.FileMode(h.Mode), h.Typeflag == tar.TypeSymlink})
		}
	}
	return entries
}
func packageWrite(t *testing.T, entries []packageEntry, isZIP bool) []byte {
	t.Helper()
	var out bytes.Buffer
	if isZIP {
		w := zip.NewWriter(&out)
		for _, e := range entries {
			h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
			mode := e.mode
			if e.link {
				mode |= os.ModeSymlink
			}
			h.SetMode(mode)
			f, err := w.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.Write(e.data); err != nil {
				t.Fatal(err)
			}
		}
		if w.Close() != nil {
			t.Fatal("zip close")
		}
	} else {
		gz := gzip.NewWriter(&out)
		w := tar.NewWriter(gz)
		for _, e := range entries {
			h := &tar.Header{Name: e.name, Mode: int64(e.mode), Size: int64(len(e.data)), Typeflag: tar.TypeReg}
			if e.link {
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "outside"
				h.Size = 0
			}
			if w.WriteHeader(h) != nil {
				t.Fatal("tar header")
			}
			if !e.link {
				if _, err := w.Write(e.data); err != nil {
					t.Fatal(err)
				}
			}
		}
		if w.Close() != nil || gz.Close() != nil {
			t.Fatal("tar close")
		}
	}
	return out.Bytes()
}
