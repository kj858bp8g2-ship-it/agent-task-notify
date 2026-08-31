// package-native is a developer-only strict candidate builder and verifier.
// It is intentionally separate from the end-user executable.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/hostfile"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/strictjson"
)

const candidateVersion = "0.2.0-rc.2"
const binaryLimit = 100 * 1024 * 1024
const textLimit = 1024 * 1024
const archiveLimit = 420 * 1024 * 1024
const expandedArchiveLimit = 421 * 1024 * 1024
const unsignedNotice = "UNSIGNED CANDIDATE — experimental CI test artifact only.\nNot signed or notarized for end-user distribution. Stop if macOS blocks execution; do not bypass Gatekeeper or remove quarantine.\nRead packaged INSTALL.md or INSTALL.zh-CN.md for the explicit experimental setup and evidence boundaries.\n"

var errPackage = errors.New("native package rejected")

type manifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	Version       string   `json:"version"`
	Platform      string   `json:"platform"`
	BinarySHA256  string   `json:"binarySHA256"`
	Files         []string `json:"files"`
}
type entry struct {
	name string
	data []byte
	mode os.FileMode
}

// Only fixed call-site labels are emitted; no input, native error or child
// output is projected. A label identifies the boundary about to be checked.
type packageDiagnostics bool

func (d packageDiagnostics) stage(label string) {
	if d {
		fmt.Fprintln(os.Stderr, "native package stage:", label)
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, errPackage)
		os.Exit(1)
	}
}

func run(args []string) error {
	diagnostics := packageDiagnostics(len(args) > 0 && (args[0] == "verify" || args[0] == "build") && os.Getenv("ATN_PACKAGE_DIAGNOSTICS") == "1")
	diagnostics.stage("arguments")
	if len(args) == 0 {
		return errPackage
	}
	var required []string
	switch args[0] {
	case "build":
		required = []string{"--source-root", "--binary", "--platform", "--version", "--output"}
	case "verify":
		required = []string{"--archive", "--checksums", "--platform", "--version", "--extract-to"}
	default:
		return errPackage
	}
	if len(args) != 1+2*len(required) {
		return errPackage
	}
	values := make(map[string]string)
	for i := 1; i < len(args); i += 2 {
		if !slices.Contains(required, args[i]) || values[args[i]] != "" || args[i+1] == "" {
			return errPackage
		}
		values[args[i]] = args[i+1]
	}
	if values["--version"] != candidateVersion || !slices.Contains([]string{"windows-amd64", "darwin-amd64", "darwin-arm64"}, values["--platform"]) {
		return errPackage
	}
	for key, value := range values {
		if key != "--version" && key != "--platform" && !absolutePath(value) {
			return errPackage
		}
	}
	if args[0] == "build" {
		return buildPackage(values, diagnostics)
	}
	return verifyPackage(values, diagnostics)
}

func absolutePath(path string) bool {
	return utf8.ValidString(path) && filepath.IsAbs(path) && filepath.Clean(path) == path && strings.IndexFunc(path, unicode.IsControl) < 0
}

func binaryName(platform string) string {
	if platform == "windows-amd64" {
		return "agent-task-notify.exe"
	}
	return "agent-task-notify"
}
func archiveName(platform string) string {
	suffix := ".tar.gz"
	if platform == "windows-amd64" {
		suffix = ".zip"
	}
	return "atn-native-" + candidateVersion + "-" + platform + suffix
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func inventory(platform string) []entry {
	binary := binaryName(platform)
	list := []entry{
		{binary, nil, 0755}, {"LICENSE", nil, 0644}, {"THIRD_PARTY_NOTICES.md", nil, 0644}, {"manifest.json", nil, 0644},
		{"INSTALL.md", nil, 0644}, {"INSTALL.zh-CN.md", nil, 0644}, {"skills/agent-task-notify/SKILL.md", nil, 0644}, {"skills/agent-task-notify/agents/openai.yaml", nil, 0644},
		{"integrations/opencode/agent-task-notify.mjs", nil, 0644}, {"integrations/opencode/bridge.mjs", nil, 0644},
		{"workbuddy/.workbuddy-plugin/plugin.json", nil, 0644}, {"workbuddy/hooks/hooks.json", nil, 0644}, {"workbuddy/hooks/launch.sh", nil, 0755}, {"workbuddy/runtime/" + binary, nil, 0755},
		{"openclaw/README.md", nil, 0644}, {"openclaw/package.json", nil, 0644}, {"openclaw/openclaw.plugin.json", nil, 0644}, {"openclaw/index.js", nil, 0644}, {"openclaw/bridge.mjs", nil, 0644}, {"openclaw/runtime/" + binary, nil, 0755},
		{"hermes/README.md", nil, 0644}, {"hermes/config.example.yaml", nil, 0644}, {"hermes/runtime/" + binary, nil, 0755},
	}
	if strings.HasPrefix(platform, "darwin-") {
		list = append(list, entry{"UNSIGNED-CANDIDATE.txt", nil, 0644})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	return list
}
func names(entries []entry) []string {
	result := make([]string, 0, len(entries))
	for _, e := range entries {
		result = append(result, e.name)
	}
	return result
}
func memberLimit(name, platform string) int64 {
	if packagedBinary(name, platform) {
		return binaryLimit
	}
	return textLimit
}

func packagedBinary(name, platform string) bool {
	binary := binaryName(platform)
	if name == binary {
		return true
	}
	for _, prefix := range []string{"workbuddy/runtime/", "openclaw/runtime/", "hermes/runtime/"} {
		if name == prefix+binary {
			return true
		}
	}
	return false
}

// Source/archives are nonsecret inputs: do not impose private state permissions
// on them. Reject links/reparse/irregular components and bound every read.
func readRegular(path string, limit int64) ([]byte, error) {
	if !absolutePath(path) {
		return nil, errPackage
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || (current != path && !info.IsDir()) {
			return nil, errPackage
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errPackage
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errPackage
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errPackage
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
		return nil, errPackage
	}
	return data, nil
}

func checkArchitecture(data []byte, platform string) error {
	if platform == "windows-amd64" {
		f, err := pe.NewFile(bytes.NewReader(data))
		if err != nil {
			return errPackage
		}
		defer f.Close()
		_, is64 := f.OptionalHeader.(*pe.OptionalHeader64)
		if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || !is64 || f.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || f.Characteristics&pe.IMAGE_FILE_DLL != 0 {
			return errPackage
		}
		return nil
	}
	f, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return errPackage
	}
	defer f.Close()
	cpu := macho.CpuAmd64
	if platform == "darwin-arm64" {
		cpu = macho.CpuArm64
	}
	if f.Magic != macho.Magic64 || f.Cpu != cpu || f.Type != macho.TypeExec {
		return errPackage
	}
	return nil
}

func buildPackage(v map[string]string, diagnostics packageDiagnostics) error {
	platform := v["--platform"]
	diagnostics.stage("build-binary-read")
	binary, err := readRegular(v["--binary"], binaryLimit)
	if err != nil {
		return errPackage
	}
	diagnostics.stage("build-architecture")
	if checkArchitecture(binary, platform) != nil {
		return errPackage
	}
	entries := inventory(platform)
	for i := range entries {
		e := &entries[i]
		source := e.name
		switch e.name {
		case binaryName(platform), "workbuddy/runtime/" + binaryName(platform), "openclaw/runtime/" + binaryName(platform), "hermes/runtime/" + binaryName(platform):
			e.data = binary
			continue
		case "manifest.json":
			continue
		case "UNSIGNED-CANDIDATE.txt":
			e.data = []byte(unsignedNotice)
			continue
		case "INSTALL.md":
			source = "docs/native-installation.md"
		case "INSTALL.zh-CN.md":
			source = "docs/native-installation.zh-CN.md"
		case "workbuddy/hooks/hooks.json":
			source = "integrations/workbuddy/native/hooks.json"
		case "workbuddy/hooks/launch.sh":
			source = "integrations/workbuddy/native/launch.sh"
		case "workbuddy/.workbuddy-plugin/plugin.json":
			source = "integrations/workbuddy/.workbuddy-plugin/plugin.json"
		case "openclaw/README.md":
			source = "integrations/openclaw/README.md"
		case "openclaw/package.json":
			source = "integrations/openclaw/package.json"
		case "openclaw/openclaw.plugin.json":
			source = "integrations/openclaw/openclaw.plugin.json"
		case "openclaw/index.js":
			source = "integrations/openclaw/index.js"
		case "openclaw/bridge.mjs":
			source = "integrations/openclaw/bridge.mjs"
		case "hermes/README.md":
			source = "integrations/hermes/README.md"
		case "hermes/config.example.yaml":
			source = "integrations/hermes/config.example.yaml"
		}
		diagnostics.stage("build-source-read")
		e.data, err = readRegular(filepath.Join(v["--source-root"], filepath.FromSlash(source)), textLimit)
		if err != nil {
			return errPackage
		}
		diagnostics.stage("build-source-utf8")
		if !utf8.Valid(e.data) {
			return errPackage
		}
		if e.name == "workbuddy/.workbuddy-plugin/plugin.json" || e.name == "openclaw/package.json" {
			diagnostics.stage("build-plugin-json")
			object, err := strictjson.Object(e.data)
			if err != nil {
				return errPackage
			}
			object["version"] = json.RawMessage(`"` + candidateVersion + `"`)
			e.data, err = json.MarshalIndent(object, "", "  ")
			if err != nil {
				return errPackage
			}
			e.data = append(e.data, '\n')
		}
		if e.name == "workbuddy/hooks/launch.sh" {
			diagnostics.stage("build-launcher-lf")
			e.data = bytes.ReplaceAll(e.data, []byte("\r\n"), []byte("\n"))
			if bytes.ContainsRune(e.data, '\r') {
				return errPackage
			}
		}
	}
	diagnostics.stage("build-manifest")
	m := manifest{1, candidateVersion, platform, digest(binary), names(entries)}
	for i := range entries {
		if entries[i].name == "manifest.json" {
			entries[i].data, err = json.MarshalIndent(m, "", "  ")
			if err != nil {
				return errPackage
			}
			entries[i].data = append(entries[i].data, '\n')
		}
	}
	diagnostics.stage("build-archive-encode")
	data, err := encodeArchive(entries, platform)
	if err != nil {
		return err
	}
	diagnostics.stage("build-output-root")
	if err := newOwnedDirectory(v["--output"], diagnostics); err != nil {
		return err
	}
	archive := filepath.Join(v["--output"], archiveName(platform))
	diagnostics.stage("build-archive-write")
	if store.WriteAtomic(archive, data) != nil {
		return errPackage
	}
	diagnostics.stage("build-checksums-write")
	if store.WriteAtomic(filepath.Join(v["--output"], "SHA256SUMS"), []byte(digest(data)+"  "+filepath.Base(archive)+"\n")) != nil {
		return errPackage
	}
	diagnostics.stage("complete")
	fmt.Println("built", filepath.Base(archive))
	return nil
}

func encodeArchive(entries []entry, platform string) ([]byte, error) {
	var out bytes.Buffer
	if platform == "windows-amd64" {
		w := zip.NewWriter(&out)
		for _, e := range entries {
			h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
			h.SetMode(e.mode)
			f, err := w.CreateHeader(h)
			if err != nil {
				return nil, errPackage
			}
			if _, err = f.Write(e.data); err != nil {
				return nil, errPackage
			}
		}
		if w.Close() != nil {
			return nil, errPackage
		}
	} else {
		gz := gzip.NewWriter(&out)
		w := tar.NewWriter(gz)
		for _, e := range entries {
			h := &tar.Header{Name: e.name, Mode: int64(e.mode), Size: int64(len(e.data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
			if w.WriteHeader(h) != nil {
				return nil, errPackage
			}
			if _, err := w.Write(e.data); err != nil {
				return nil, errPackage
			}
		}
		if w.Close() != nil || gz.Close() != nil {
			return nil, errPackage
		}
	}
	return out.Bytes(), nil
}

// expandedReader bounds bytes delivered to tar, including metadata consumed
// inside Next. At the boundary a one-byte probe distinguishes genuine gzip EOF
// (which validates the trailer) from limit exhaustion, never a successful EOF.
type expandedReader struct {
	r         io.Reader
	remaining int64
}

func (r *expandedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n != 0 {
			return 0, errPackage
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func decodeArchive(data []byte, platform string) ([]entry, error) {
	return decodeArchiveWithExpandedLimit(data, platform, expandedArchiveLimit)
}

func decodeArchiveWithExpandedLimit(data []byte, platform string, limit int64) ([]entry, error) {
	if limit < 0 {
		return nil, errPackage
	}
	want := inventory(platform)
	found := make(map[string]entry)
	add := func(name string, mode os.FileMode, size int64, r io.Reader) error {
		index := slices.IndexFunc(want, func(e entry) bool { return e.name == name })
		if index < 0 || found[name].name != "" || mode != want[index].mode || size < 0 || size > memberLimit(name, platform) {
			return errPackage
		}
		content, err := io.ReadAll(io.LimitReader(r, size+1))
		if err != nil || int64(len(content)) != size {
			return errPackage
		}
		if memberLimit(name, platform) == textLimit && !utf8.Valid(content) {
			return errPackage
		}
		found[name] = entry{name, content, mode}
		return nil
	}
	if platform == "windows-amd64" {
		r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil || len(r.File) != len(want) || r.Comment != "" {
			return nil, errPackage
		}
		for _, f := range r.File {
			if !f.Mode().IsRegular() || f.UncompressedSize64 > uint64(memberLimit(f.Name, platform)) {
				return nil, errPackage
			}
			reader, err := f.Open()
			if err != nil {
				return nil, errPackage
			}
			err = add(f.Name, f.Mode(), int64(f.UncompressedSize64), reader)
			reader.Close()
			if err != nil {
				return nil, errPackage
			}
		}
	} else {
		input := bytes.NewReader(data)
		gz, err := gzip.NewReader(input)
		if err != nil {
			return nil, errPackage
		}
		defer gz.Close()
		gz.Multistream(false)
		expanded := &expandedReader{r: gz, remaining: limit}
		r := tar.NewReader(expanded)
		for {
			h, err := r.Next()
			if err == io.EOF {
				break
			}
			if err != nil || h.Typeflag != tar.TypeReg || h.Linkname != "" || len(h.PAXRecords) != 0 || h.Uid != 0 || h.Gid != 0 || h.Mode < 0 || h.Mode > 0777 {
				return nil, errPackage
			}
			if add(h.Name, os.FileMode(h.Mode), h.Size, r) != nil {
				return nil, errPackage
			}
		}
		rest, err := io.ReadAll(io.LimitReader(expanded, 1))
		if err != nil || len(rest) != 0 || input.Len() != 0 {
			return nil, errPackage
		}
	}
	if len(found) != len(want) {
		return nil, errPackage
	}
	for i := range want {
		want[i] = found[want[i].name]
	}
	return want, nil
}

func verifyContents(entries []entry, platform string) error {
	values := make(map[string][]byte)
	for _, e := range entries {
		values[e.name] = e.data
	}
	data := values["manifest.json"]
	if _, err := strictjson.Object(data); err != nil {
		return errPackage
	}
	var m manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil || m.SchemaVersion != 1 || m.Version != candidateVersion || m.Platform != platform || !slices.Equal(m.Files, names(inventory(platform))) {
		return errPackage
	}
	binary := values[binaryName(platform)]
	if m.BinarySHA256 != digest(binary) || checkArchitecture(binary, platform) != nil {
		return errPackage
	}
	for _, prefix := range []string{"workbuddy/runtime/", "openclaw/runtime/", "hermes/runtime/"} {
		if !bytes.Equal(binary, values[prefix+binaryName(platform)]) {
			return errPackage
		}
	}
	for _, name := range []string{"openclaw/package.json", "openclaw/openclaw.plugin.json"} {
		if _, err := strictjson.Object(values[name]); err != nil {
			return errPackage
		}
	}
	if strings.HasPrefix(platform, "darwin-") && !bytes.Equal(values["UNSIGNED-CANDIDATE.txt"], []byte(unsignedNotice)) {
		return errPackage
	}
	if bytes.ContainsRune(values["workbuddy/hooks/launch.sh"], '\r') {
		return errPackage
	}
	return nil
}

// Never repair an existing object. The store creates new objects with explicit
// current-user ownership even when Windows defaults new objects to Administrators.
func newOwnedDirectory(path string, diagnostics packageDiagnostics) error {
	diagnostics.stage("directory-parent")
	if !absolutePath(path) {
		return errPackage
	}
	stage, category, err := store.CheckPrivateDirectoryParentDiagnostic(path)
	if err != nil {
		if diagnostics {
			// Both fields are fixed code-owned labels from this same check,
			// never input, a native error, or a second filesystem scan.
			fmt.Fprintf(os.Stderr, "native package parent: stage=%s category=%s\n", stage, category)
		}
		return errPackage
	}
	diagnostics.stage("directory-create")
	if store.EnsurePrivateDirectory(path) != nil {
		return errPackage
	}
	return nil
}

func verifyPackage(v map[string]string, diagnostics packageDiagnostics) error {
	platform := v["--platform"]
	diagnostics.stage("target")
	if platform != runtime.GOOS+"-"+runtime.GOARCH || filepath.Base(v["--archive"]) != archiveName(platform) {
		return errPackage
	}
	diagnostics.stage("archive-read")
	data, err := readRegular(v["--archive"], archiveLimit)
	if err != nil {
		return err
	}
	diagnostics.stage("checksums-read")
	sums, err := readRegular(v["--checksums"], textLimit)
	if err != nil {
		return errPackage
	}
	diagnostics.stage("checksum")
	if string(sums) != digest(data)+"  "+archiveName(platform)+"\n" {
		return errPackage
	}
	diagnostics.stage("archive-decode")
	entries, err := decodeArchive(data, platform)
	if err != nil {
		return errPackage
	}
	diagnostics.stage("content")
	if verifyContents(entries, platform) != nil {
		return errPackage
	}
	root := v["--extract-to"]
	diagnostics.stage("extract-root")
	if newOwnedDirectory(root, diagnostics) != nil {
		return errPackage
	}
	created := map[string]bool{root: true}
	for _, e := range entries {
		path := filepath.Join(root, filepath.FromSlash(e.name))
		parent := filepath.Dir(path)
		var missing []string
		for current := parent; !created[current]; current = filepath.Dir(current) {
			if current == filepath.Dir(current) {
				return errPackage
			}
			missing = append(missing, current)
		}
		for i := len(missing) - 1; i >= 0; i-- {
			diagnostics.stage("extract-directory")
			if newOwnedDirectory(missing[i], diagnostics) != nil {
				return errPackage
			}
			created[missing[i]] = true
		}
		diagnostics.stage("extract-write")
		if _, err := os.Lstat(path); !os.IsNotExist(err) || store.WriteAtomic(path, e.data) != nil {
			return errPackage
		}
		diagnostics.stage("private-readback")
		private, err := store.ReadPrivate(path, memberLimit(e.name, platform))
		if err != nil || !bytes.Equal(private, e.data) {
			return errPackage
		}
		// Darwin private data is strictly0600. Only after private readback may
		// these new objects receive archive mode, then hostfile validates them.
		diagnostics.stage("mode")
		if os.Chmod(path, e.mode) != nil {
			return errPackage
		}
		diagnostics.stage("hostfile")
		if checkExtracted(path, e, platform) != nil {
			return errPackage
		}
	}
	// Recheck the entire exact list before any execution, including both binaries.
	diagnostics.stage("extract-recheck")
	for _, e := range entries {
		if checkExtracted(filepath.Join(root, filepath.FromSlash(e.name)), e, platform) != nil {
			return errPackage
		}
	}
	if runIsolated(root, platform, entries, diagnostics) != nil {
		return errPackage
	}
	diagnostics.stage("complete")
	fmt.Println("verified agent-task-notify", candidateVersion, platform, "— doctor and eight dry previews passed")
	return nil
}

func checkExtracted(path string, e entry, platform string) error {
	snapshot, err := hostfile.Read(path, memberLimit(e.name, platform))
	if err != nil || !snapshot.Exists || !bytes.Equal(snapshot.Data, e.data) {
		return errPackage
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS == "darwin" && info.Mode().Perm() != e.mode) {
		return errPackage
	}
	return nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64*1024 {
		return 0, errPackage
	}
	return b.Buffer.Write(p)
}

// This is isolation of normal CLI behavior, not a sandbox for hostile code.
// Only version/doctor/dry previews run: no configure, install, send or Keychain.
func runIsolated(root, platform string, entries []entry, diagnostics packageDiagnostics) error {
	var nonce [16]byte
	diagnostics.stage("isolated-random")
	if _, err := rand.Read(nonce[:]); err != nil {
		return errPackage
	}
	runRoot := filepath.Join(filepath.Dir(root), "atn-package-check-"+hex.EncodeToString(nonce[:]))
	diagnostics.stage("isolated-root")
	if newOwnedDirectory(runRoot, diagnostics) != nil {
		return errPackage
	}
	dirs := []string{"home", "profile", "app-data", "local-data", "xdg-config", "xdg-data", "tmp", "cwd"}
	defer func() {
		for i := len(dirs) - 1; i >= 0; i-- {
			_ = os.Remove(filepath.Join(runRoot, dirs[i]))
		}
		_ = os.Remove(runRoot)
	}()
	for _, d := range dirs {
		diagnostics.stage("isolated-directory")
		if newOwnedDirectory(filepath.Join(runRoot, d), diagnostics) != nil {
			return errPackage
		}
	}
	env := []string{"PATH=", "HOME=" + filepath.Join(runRoot, "home"), "USERPROFILE=" + filepath.Join(runRoot, "profile"), "APPDATA=" + filepath.Join(runRoot, "app-data"), "LOCALAPPDATA=" + filepath.Join(runRoot, "local-data"), "XDG_CONFIG_HOME=" + filepath.Join(runRoot, "xdg-config"), "XDG_DATA_HOME=" + filepath.Join(runRoot, "xdg-data"), "TMP=" + filepath.Join(runRoot, "tmp"), "TEMP=" + filepath.Join(runRoot, "tmp"), "TMPDIR=" + filepath.Join(runRoot, "tmp"), "ATN_DATA_DIRECTORY=" + filepath.Join(runRoot, "data")}
	if runtime.GOOS == "windows" {
		for _, key := range []string{"SystemRoot", "WINDIR"} {
			if value := os.Getenv(key); value != "" {
				env = append(env, key+"="+value)
			}
		}
	}
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(root, binaryName(platform)), args...)
		cmd.Dir = filepath.Join(runRoot, "cwd")
		cmd.Env = env
		var out, errors boundedOutput
		cmd.Stdout = &out
		cmd.Stderr = &errors
		cmd.WaitDelay = time.Second
		if cmd.Run() != nil || errors.Len() != 0 {
			return nil, errPackage
		}
		return out.Bytes(), nil
	}
	diagnostics.stage("isolated-version")
	version, err := run("version")
	if err != nil {
		return errPackage
	}
	diagnostics.stage("isolated-version-output")
	if string(version) != "agent-task-notify "+candidateVersion+" "+strings.ReplaceAll(platform, "-", "/")+"\n" {
		return errPackage
	}
	diagnostics.stage("isolated-doctor")
	doctor, err := run("doctor", "--data-directory", filepath.Join(runRoot, "data"))
	if err != nil {
		return err
	}
	var d struct {
		SchemaVersion int  `json:"schemaVersion"`
		Configured    bool `json:"configured"`
		InputErrors   int  `json:"inputErrors"`
	}
	diagnostics.stage("isolated-doctor-output")
	if json.Unmarshal(doctor, &d) != nil || d.SchemaVersion != 1 || d.Configured || d.InputErrors != 0 {
		return errPackage
	}
	for _, agent := range []string{"codex", "claude-code", "cursor", "gemini-cli", "opencode", "workbuddy", "openclaw", "hermes"} {
		diagnostics.stage("isolated-preview")
		out, err := run("preview", "--agent", agent, "--data-directory", filepath.Join(runRoot, "data"))
		if err != nil {
			return err
		}
		var p struct {
			Provider   string `json:"provider"`
			Agent      string `json:"agent"`
			Ring       int    `json:"ringTargetSeconds"`
			Continuous bool   `json:"continuous"`
		}
		diagnostics.stage("isolated-preview-output")
		if json.Unmarshal(out, &p) != nil || p.Provider != "bark" || p.Agent != agent || p.Ring != 45 || !p.Continuous {
			return errPackage
		}
	}
	diagnostics.stage("isolated-state")
	children, err := os.ReadDir(runRoot)
	if err != nil || len(children) != len(dirs) {
		return errPackage
	}
	for _, d := range dirs {
		children, err := os.ReadDir(filepath.Join(runRoot, d))
		if err != nil || len(children) != 0 {
			return errPackage
		}
	}
	diagnostics.stage("final-recheck")
	for _, e := range entries {
		if checkExtracted(filepath.Join(root, filepath.FromSlash(e.name)), e, platform) != nil {
			return errPackage
		}
	}
	return nil
}
