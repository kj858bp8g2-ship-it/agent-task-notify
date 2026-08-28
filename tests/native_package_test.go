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
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/core"
)

func nativeSourceRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source location unavailable")
	}
	root := filepath.Dir(filepath.Dir(file))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	return root
}

func nativeSourceText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(nativeSourceRoot(t), filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// Installed guidance is an interface: missing commands/provider/platform
// boundaries or a broken local link would strand an offline package user.
func TestNativeGuideContract(t *testing.T) {
	compat := nativeSourceText(t, "docs/native-compatibility.md")
	for _, agent := range core.Agents() {
		if !strings.Contains(compat, "`"+agent.ID+"`") {
			t.Errorf("missing compatibility ID %s", agent.ID)
		}
	}
	for _, path := range []string{"docs/native-installation.md", "skills/agent-task-notify/SKILL.md"} {
		guide := nativeSourceText(t, path)
		for _, value := range []string{"0.2.0-rc.1", "Windows", "Mac", "experimental", "bark", "ntfy", "Android", "iOS", "version", "configure", "doctor", "preview", "install", "uninstall", "--data-directory", "--send", "--apply", "1800", "3600", "45", "60", "alarm", "needs_attention", "chat"} {
			if !strings.Contains(guide, value) {
				t.Errorf("%s missing installed contract %q", path, value)
			}
		}
		for _, agent := range core.Agents() {
			if !strings.Contains(guide, "`"+agent.ID+"`") {
				t.Errorf("%s missing ID %s", path, agent.ID)
			}
		}
		if strings.Contains(guide, "transitional legacy") || strings.Contains(guide, "../../README.md") {
			t.Errorf("%s not native/self-contained", path)
		}
	}
}

// This intentionally checks the known YAML layout, not arbitrary YAML. Any
// new trigger/job/permission must receive review rather than escape the gate.
func TestNativeReleaseWorkflowContract(t *testing.T) {
	source := nativeSourceText(t, ".github/workflows/native-release.yml")
	pre, jobs, ok := strings.Cut(source, "\njobs:\n")
	if !ok || !strings.Contains(pre, "on:\n  push:\n    tags: ['v0.2.0-rc.1']\n  workflow_dispatch:\n") || !strings.Contains(pre, "permissions:\n  contents: read\n") {
		t.Fatal("release triggers/default permission not exact")
	}
	for _, banned := range []string{"branches:", "pull_request", "schedule:", "workflow_call:", "write-all", "secrets:", "continue-on-error", "always()", "quarantine", "xattr", "spctl", "--clobber"} {
		if strings.Contains(source, banned) {
			t.Errorf("unsafe release capability %q", banned)
		}
	}
	if strings.Count(jobs, "\n  ") == 0 {
		t.Fatal("missing release jobs")
	}
	names := regexp.MustCompile(`(?m)^  ([a-z-]+):$`).FindAllStringSubmatch(jobs, -1)
	var got []string
	for _, name := range names {
		got = append(got, name[1])
	}
	if !reflect.DeepEqual(got, []string{"native", "legacy", "publish"}) {
		t.Fatalf("unreviewed job set: %v", got)
	}
	for _, job := range []struct{ name, path string }{{"native", "native.yml"}, {"legacy", "test.yml"}} {
		want := "  " + job.name + ":\n    if: github.ref == 'refs/tags/v0.2.0-rc.1'\n    uses: ./.github/workflows/" + job.path + "\n"
		if !strings.Contains(jobs, want) {
			t.Errorf("missing exact-tag reusable gate %s", job.name)
		}
	}
	_, publish, _ := strings.Cut(jobs, "  publish:\n")
	if !strings.Contains(publish, "    needs: [native, legacy]\n    if: github.ref == 'refs/tags/v0.2.0-rc.1' && success()\n    permissions:\n      contents: write\n") || strings.Count(source, "contents: write") != 1 {
		t.Fatal("publication must be the only writer and depend on both complete gates")
	}
	if strings.Count(publish, "actions/download-artifact@018cc2cf5baa6db3ef3c5f8a56943fffe632ef53") != 3 || !strings.Contains(publish, "actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803") {
		t.Fatal("pinned exact downloads/checkout missing")
	}
	for _, platform := range []string{"windows-amd64", "darwin-amd64", "darwin-arm64"} {
		if strings.Count(publish, "name: native-candidate-"+platform+"\n") != 1 {
			t.Fatal("canonical artifact identity missing")
		}
	}
	if strings.Count(publish, "GH_TOKEN:") != 1 || !strings.Contains(publish, "GH_TOKEN: ${{ github.token }}") {
		t.Fatal("built-in job token must be step-scoped")
	}
	native := nativeSourceText(t, ".github/workflows/native.yml")
	legacy := nativeSourceText(t, ".github/workflows/test.yml")
	if !strings.Contains(native, "      - main\n") || !strings.Contains(native, "  workflow_call:\n") || !strings.Contains(legacy, "  workflow_call:\n") || !strings.Contains(legacy, "  push:\n  pull_request:\n") {
		t.Fatal("reusable/full main gates missing")
	}
}

func nativeWorkflowRun(t *testing.T, path, step string) string {
	t.Helper()
	source := nativeSourceText(t, path)
	_, block, ok := strings.Cut(source, "      - name: "+step+"\n")
	if !ok {
		t.Fatal("workflow step missing", step)
	}
	block = strings.SplitN(block, "\n      - ", 2)[0]
	_, body, ok := strings.Cut(block, "        run: |\n")
	if !ok {
		t.Fatal("literal script missing", step)
	}
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		if !strings.HasPrefix(line, "          ") {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, "          "))
	}
	return strings.Join(lines, "\n") + "\n"
}

func nativeReleasePython(t *testing.T, script, dir string, args ...string) (string, error) {
	t.Helper()
	python := "python3"
	if runtime.GOOS == "windows" {
		python = "python"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, append([]string{"-I", "-c", script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(withoutEnvironmentNames(os.Environ(), "GITHUB_REF", "GITHUB_REPOSITORY", "GH_TOKEN"), "GITHUB_REF=refs/tags/v0.2.0-rc.1", "GITHUB_REPOSITORY=kj858bp8g2-ship-it/agent-task-notify")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestNativeReleasePublicationPlan(t *testing.T) {
	script := nativeWorkflowRun(t, ".github/workflows/native-release.yml", "Publish only a confirmed absent prerelease")
	// Only gh is replaced: no network, token, release, or tag mutations. The
	// actual policy must reject auth/network/unknown/exists/race responses.
	harness := `import subprocess, sys, json, os
scenario = sys.argv[1]
calls = []
def fake_run(argv, **kwargs):
    calls.append(argv)
    assert argv[0] == "gh" and kwargs.get("capture_output") and kwargs.get("timeout")
    if argv[1:3] == ["release", "view"]:
        return subprocess.CompletedProcess(argv, 0 if scenario == "exists" else 1, '{"tagName":"v0.2.0-rc.1"}', '')
    if argv[1:3] == ["api", "--method"] and argv[-1] == "repos/kj858bp8g2-ship-it/agent-task-notify":
        return subprocess.CompletedProcess(argv, 1 if scenario == "auth" else 0, '{"full_name":"kj858bp8g2-ship-it/agent-task-notify"}', '')
    if argv[1] == "api":
        status = {"absent":404, "create-fails":404, "race":200, "network":0, "unknown":500, "forbidden":403, "bad404":404}[scenario]
        body = '{"message":"Not Found"}' if scenario != "bad404" else 'unclassified'
        return subprocess.CompletedProcess(argv, 1 if status != 200 else 0, 'HTTP/2.0 '+str(status)+'\n\n'+body, '')
    if argv[1:3] == ["release", "create"]:
        return subprocess.CompletedProcess(argv, 1 if scenario == "create-fails" else 0, '', '')
    raise AssertionError(argv)
subprocess.run = fake_run
if scenario == "wrong-ref": os.environ["GITHUB_REF"] = "refs/heads/main"
if scenario == "wrong-repo": os.environ["GITHUB_REPOSITORY"] = "other/repo"
try:
    exec(SCRIPT, {})
    success = True
except SystemExit as error:
    success = error.code in (None, 0)
assert success == (scenario == "absent"), (scenario, success, calls)
creates = [c for c in calls if c[1:3] == ["release", "create"]]
assert len(creates) == (1 if scenario in ("absent", "create-fails") else 0), calls
if creates:
    c = creates[0]
    assert c[3] == "v0.2.0-rc.1" and "--verify-tag" in c and "--prerelease" in c
    assert c[c.index("--repo")+1] == "kj858bp8g2-ship-it/agent-task-notify"
    for name in ("atn-native-0.2.0-rc.1-windows-amd64.zip", "atn-native-0.2.0-rc.1-darwin-amd64.tar.gz", "atn-native-0.2.0-rc.1-darwin-arm64.tar.gz"):
        assert sum(a.endswith('/'+name) for a in c) == 1, c
    assert "SHA256SUMS" in c and "--clobber" not in c
print('publication policy passed: '+scenario)
`
	encoded, _ := json.Marshal(script)
	harness = "SCRIPT = " + string(encoded) + "\n" + harness
	for _, scenario := range []string{"absent", "exists", "auth", "network", "unknown", "forbidden", "bad404", "race", "create-fails", "wrong-ref", "wrong-repo"} {
		t.Run(scenario, func(t *testing.T) {
			if output, err := nativeReleasePython(t, harness, t.TempDir(), scenario); err != nil {
				t.Fatalf("publication policy: %v %s", err, output)
			}
		})
	}
}

func TestNativeReleaseArchiveInspection(t *testing.T) {
	script := nativeWorkflowRun(t, ".github/workflows/native-release.yml", "Inspect exact source and canonical archives")
	for _, fault := range []string{"none", "source-version", "duplicate-version", "checksum", "extra-asset", "wrong-name", "manifest-version", "manifest-platform", "manifest-files", "duplicate-member", "traversal", "link", "mode", "binary-digest"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0700); err != nil {
				t.Fatal(err)
			}
			source := "package cli\nconst Version = \"0.2.0-rc.1\"\n"
			if fault == "source-version" {
				source = "package cli\nconst Version = \"0.2.0-dev\"\n"
			}
			if fault == "duplicate-version" {
				source += "const Version = \"0.2.0-rc.1\"\n"
			}
			if err := os.WriteFile(filepath.Join(dir, "internal", "cli", "app.go"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			var expectedSums []string
			for _, platform := range []string{"windows-amd64", "darwin-amd64", "darwin-arm64"} {
				binary, suffix := "agent-task-notify", ".tar.gz"
				if platform == "windows-amd64" {
					binary, suffix = binary+".exe", ".zip"
				}
				asset := "atn-native-0.2.0-rc.1-" + platform + suffix
				out := filepath.Join(dir, "candidate", platform)
				if err := os.MkdirAll(out, 0700); err != nil {
					t.Fatal(err)
				}
				var entries []packageEntry
				for _, name := range []string{binary, "LICENSE", "THIRD_PARTY_NOTICES.md", "manifest.json", "INSTALL.md", "INSTALL.zh-CN.md", "skills/agent-task-notify/SKILL.md", "skills/agent-task-notify/agents/openai.yaml", "integrations/opencode/agent-task-notify.mjs", "integrations/opencode/bridge.mjs", "workbuddy/.workbuddy-plugin/plugin.json", "workbuddy/hooks/hooks.json", "workbuddy/hooks/launch.sh", "workbuddy/runtime/" + binary} {
					mode := os.FileMode(0644)
					if name == binary || name == "workbuddy/runtime/"+binary || name == "workbuddy/hooks/launch.sh" {
						mode = 0755
					}
					entries = append(entries, packageEntry{name, []byte("synthetic non-executable fixture"), mode, false})
				}
				if platform != "windows-amd64" {
					entries = append(entries, packageEntry{"UNSIGNED-CANDIDATE.txt", []byte("UNSIGNED and NOT NOTARIZED"), 0644, false})
				}
				var names []string
				for _, entry := range entries {
					names = append(names, entry.name)
				}
				sort.Strings(names)
				digest := sha256.Sum256(entries[0].data)
				manifest := map[string]any{"schemaVersion": 1, "version": "0.2.0-rc.1", "platform": platform, "binarySHA256": hex.EncodeToString(digest[:]), "files": names}
				if platform == "windows-amd64" {
					switch fault {
					case "manifest-version":
						manifest["version"] = "0.2.0-dev"
					case "manifest-platform":
						manifest["platform"] = "darwin-arm64"
					case "manifest-files":
						manifest["files"] = []string{"manifest.json"}
					case "binary-digest":
						manifest["binarySHA256"] = strings.Repeat("0", 64)
					case "duplicate-member":
						entries = append(entries, entries[0])
					case "traversal":
						entries[0].name = "../outside"
					case "link":
						entries[0].link = true
					case "mode":
						entries[0].mode = 0644
					}
				}
				encoded, _ := json.Marshal(manifest)
				for i := range entries {
					if entries[i].name == "manifest.json" {
						entries[i].data = encoded
					}
				}
				archive := packageWrite(t, entries, platform == "windows-amd64")
				hash := sha256.Sum256(archive)
				checksum := hex.EncodeToString(hash[:]) + "  " + asset + "\n"
				expectedSums = append(expectedSums, checksum)
				if platform == "windows-amd64" {
					if fault == "checksum" {
						checksum = strings.Repeat("0", 64) + "  " + asset + "\n"
					}
					if fault == "wrong-name" {
						asset = "unexpected.zip"
					}
					if fault == "extra-asset" {
						if err := os.WriteFile(filepath.Join(out, "unexpected.txt"), []byte("extra"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := os.WriteFile(filepath.Join(out, asset), archive, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(checksum), 0600); err != nil {
					t.Fatal(err)
				}
			}
			output, err := nativeReleasePython(t, script, dir)
			if (err == nil) != (fault == "none") {
				t.Fatalf("inspection fault=%s: %v %s", fault, err, output)
			}
			combined, readErr := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
			if fault == "none" {
				if readErr != nil || string(combined) != strings.Join(expectedSums, "") {
					t.Fatal("combined checksums not exact")
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatal("rejected inspection left publishable checksums")
			}
			if _, err := os.Stat(filepath.Join(dir, "outside")); !os.IsNotExist(err) {
				t.Fatal("inspection extracted archive")
			}
		})
	}
}

type packageEntry struct {
	name string
	data []byte
	mode os.FileMode
	link bool
}

// Removing any safety gate, leaking extra source files, or running the source
// binary instead of the archived executable must fail these black-box checks.
func TestNativePackage(t *testing.T) {
	t.Setenv("ATN_PACKAGE_DIAGNOSTICS", "")
	root := nativeSourceRoot(t)
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
	runner := "macOS"
	if runtime.GOOS == "windows" {
		runner = "Windows"
	}
	selected, err := nativeCISelectedPath(t, "Build canonical unsigned candidate archive", "candidate", runner, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal("actual package caller selection", err, selected)
	}
	out := strings.TrimSpace(selected)
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
			if !bytes.Contains(e.data, []byte("UNSIGNED")) || !bytes.Contains(e.data, []byte("INSTALL")) || bytes.Contains(e.data, []byte("transitional")) {
				t.Fatal("unsigned marker missing")
			}
		case "skills/agent-task-notify/SKILL.md", "INSTALL.md", "INSTALL.zh-CN.md":
			if !bytes.Contains(e.data, []byte("0.2.0-rc.1")) || !bytes.Contains(e.data, []byte("--data-directory")) || !bytes.Contains(e.data, []byte("--send")) {
				t.Fatal("packaged native guide missing", e.name)
			}
			for _, match := range regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`).FindAllSubmatch(e.data, -1) {
				link := string(match[1])
				if strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "#") {
					continue
				}
				target := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(e.name), link)))
				if !slices.Contains(want, target) {
					t.Fatal("broken packaged relative link", e.name, link)
				}
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
	selected, err = nativeCISelectedPath(t, "Verify and run the downloaded canonical archive", "extract", runner, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal("actual verifier caller selection", err, selected)
	}
	extract := strings.TrimSpace(selected)
	if runtime.GOOS == "darwin" {
		// A POSIX filename may contain a literal backslash; Windows root
		// normalization must not redirect this valid packaged location.
		extract += `\literal`
	}
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
	t.Run("diagnostics", func(t *testing.T) {
		run := func(t *testing.T, success bool, arguments ...string) (string, string) {
			t.Helper()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, tool, arguments...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if success {
				if err != nil {
					t.Fatalf("diagnostic success: %v %q", err, stderr.String())
				}
			} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || stdout.Len() != 0 {
				t.Fatalf("diagnostic refusal: %v stdout=%q", err, stdout.String())
			}
			return stdout.String(), stderr.String()
		}
		for _, value := range []string{"unset", "", "0", "true", " 1", "1 ", "SENSITIVE", "1"} {
			t.Run("switch-"+value, func(t *testing.T) {
				t.Setenv("ATN_PACKAGE_DIAGNOSTICS", value)
				if value == "unset" {
					if err := os.Unsetenv("ATN_PACKAGE_DIAGNOSTICS"); err != nil {
						t.Fatal(err)
					}
				}
				want := "native package rejected\n"
				if value == "1" {
					want = "native package stage: arguments\n" + want
				}
				for _, operation := range []string{"verify", "build"} {
					_, stderr := run(t, false, operation, "--SENSITIVE")
					if stderr != want {
						t.Fatalf("%s diagnostic switch stderr=%q want=%q", operation, stderr, want)
					}
				}
			})
		}
		t.Setenv("ATN_PACKAGE_DIAGNOSTICS", "1")
		t.Run("directory-parent", func(t *testing.T) {
			// A missing detail, an unconditional projection, or a second
			// mutating validation must fail through the actual command.
			for _, operation := range []struct {
				name string
				args []string
			}{{"build", args}, {"verify", verifyArgs}} {
				t.Run(operation.name, func(t *testing.T) {
					for _, kind := range []string{"existing-leaf", "missing-parent"} {
						t.Run(kind, func(t *testing.T) {
							for _, value := range []string{"unset", "", "0", "true", " 1", "1 ", "SENSITIVE", "1"} {
								t.Run("switch-"+value, func(t *testing.T) {
									t.Setenv("ATN_PACKAGE_DIAGNOSTICS", value)
									if value == "unset" {
										if err := os.Unsetenv("ATN_PACKAGE_DIAGNOSTICS"); err != nil {
											t.Fatal(err)
										}
									}
									dir := t.TempDir()
									dest := filepath.Join(dir, "SENSITIVE output")
									detail := "native package parent: stage=leaf-missing category=exists\n"
									if kind == "existing-leaf" {
										if os.Mkdir(dest, 0700) != nil || os.WriteFile(filepath.Join(dest, "keep"), []byte("SENSITIVE unchanged"), 0600) != nil {
											t.Fatal("existing parent fixture")
										}
									} else {
										dest = filepath.Join(dir, "SENSITIVE missing", "child")
										detail = "native package parent: stage=ancestor-stat category=missing\n"
									}
									v := append([]string(nil), operation.args...)
									v[len(v)-1] = dest
									_, stderr := run(t, false, v...)
									if value == "1" {
										want := "native package stage: directory-parent\n" + detail + "native package rejected\n"
										if !strings.HasSuffix(stderr, want) || strings.Count(stderr, "native package parent:") != 1 || strings.Contains(stderr, "directory-create") || strings.Contains(stderr, "SENSITIVE") {
											t.Fatalf("wrong parent diagnostic: %q", stderr)
										}
									} else if stderr != "native package rejected\n" {
										t.Fatalf("off parent diagnostic: %q", stderr)
									}
									children, err := os.ReadDir(dir)
									if err != nil {
										t.Fatal("parent fixture unreadable")
									}
									if kind == "existing-leaf" {
										kept, err := os.ReadFile(filepath.Join(dest, "keep"))
										inside, listErr := os.ReadDir(dest)
										if len(children) != 1 || listErr != nil || len(inside) != 1 || err != nil || string(kept) != "SENSITIVE unchanged" {
											t.Fatal("existing parent fixture changed")
										}
									} else if len(children) != 0 {
										t.Fatal("diagnostic created a missing parent")
									}
								})
							}
						})
					}
				})
			}
		})
		t.Run("build-off-default", func(t *testing.T) {
			for _, value := range []string{"unset", "0"} {
				t.Run(value, func(t *testing.T) {
					t.Setenv("ATN_PACKAGE_DIAGNOSTICS", value)
					if value == "unset" {
						if err := os.Unsetenv("ATN_PACKAGE_DIAGNOSTICS"); err != nil {
							t.Fatal(err)
						}
					}
					buildArgs := append([]string(nil), args...)
					buildArgs[len(buildArgs)-1] = filepath.Join(t.TempDir(), "new-build")
					stdout, stderr := run(t, true, buildArgs...)
					if stdout != "built "+name+"\n" || stderr != "" {
						t.Fatalf("build behavior changed: %q %q", stdout, stderr)
					}
				})
			}
		})
		t.Run("build-opt-in", func(t *testing.T) {
			// Catch a missing/late stage, a failed check that continues, or
			// input/native-error disclosure through real builder subprocesses.
			prefix := "native package stage: arguments\nnative package stage: build-binary-read\nnative package stage: build-architecture\n"
			sourcePair := "native package stage: build-source-read\nnative package stage: build-source-utf8\n"
			// The fixed inventory has eleven source reads: the ninth parses
			// the plugin descriptor and the eleventh checks launcher LF.
			sourceSteps := strings.Repeat(sourcePair, 9) + "native package stage: build-plugin-json\n" + strings.Repeat(sourcePair, 2) + "native package stage: build-launcher-lf\n"
			for _, kind := range []string{"binary-read", "architecture", "source-read", "source-utf8", "plugin-json", "launcher-lf", "output-parent", "success"} {
				t.Run(kind, func(t *testing.T) {
					v := append([]string(nil), args...)
					v[len(v)-1] = filepath.Join(t.TempDir(), "SENSITIVE new-build")
					want := prefix
					switch kind {
					case "binary-read":
						v[4] = filepath.Join(t.TempDir(), "SENSITIVE absent-binary")
						want = "native package stage: arguments\nnative package stage: build-binary-read\n"
					case "architecture":
						v[4] = filepath.Join(t.TempDir(), "SENSITIVE invalid-binary")
						if os.WriteFile(v[4], []byte("SENSITIVE not executable"), 0600) != nil {
							t.Fatal("diagnostic binary fixture")
						}
					case "source-read":
						v[2] = filepath.Join(t.TempDir(), "SENSITIVE missing-source")
						want += "native package stage: build-source-read\n"
					case "source-utf8", "plugin-json", "launcher-lf":
						// Copy only these nonsecret source members into a new
						// test-owned root; never mutate the real source tree.
						v[2] = filepath.Join(t.TempDir(), "SENSITIVE source")
						for _, relative := range []string{
							"docs/native-installation.md", "docs/native-installation.zh-CN.md", "LICENSE", "THIRD_PARTY_NOTICES.md",
							"integrations/opencode/agent-task-notify.mjs", "integrations/opencode/bridge.mjs",
							"skills/agent-task-notify/SKILL.md", "skills/agent-task-notify/agents/openai.yaml",
							"integrations/workbuddy/.workbuddy-plugin/plugin.json", "integrations/workbuddy/native/hooks.json", "integrations/workbuddy/native/launch.sh",
						} {
							data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
							if err != nil {
								t.Fatal("diagnostic source read")
							}
							switch {
							case kind == "source-utf8" && relative == "docs/native-installation.md":
								data = []byte{0xff}
							case kind == "plugin-json" && relative == "integrations/workbuddy/.workbuddy-plugin/plugin.json":
								data = []byte("SENSITIVE invalid plugin")
							case kind == "launcher-lf" && relative == "integrations/workbuddy/native/launch.sh":
								data = []byte("SENSITIVE invalid\rlauncher")
							}
							path := filepath.Join(v[2], filepath.FromSlash(relative))
							if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, data, 0600) != nil {
								t.Fatal("diagnostic source fixture")
							}
						}
						switch kind {
						case "source-utf8":
							want += sourcePair
						case "plugin-json":
							want += strings.Repeat(sourcePair, 9) + "native package stage: build-plugin-json\n"
						case "launcher-lf":
							want += sourceSteps
						}
					case "output-parent", "success":
						want += sourceSteps + "native package stage: build-manifest\nnative package stage: build-archive-encode\nnative package stage: build-output-root\nnative package stage: directory-parent\n"
						if kind == "output-parent" {
							v[len(v)-1] = filepath.Clean(t.TempDir())
							want += "native package parent: stage=leaf-missing category=exists\n"
						} else {
							want += "native package stage: directory-create\nnative package stage: build-archive-write\nnative package stage: build-checksums-write\nnative package stage: complete\n"
						}
					}
					stdout, stderr := run(t, kind == "success", v...)
					if kind != "success" {
						want += "native package rejected\n"
					}
					if stderr != want || strings.Count(stderr, "\n") > 40 {
						t.Fatalf("wrong build diagnostic boundary: %q", stderr)
					}
					if kind == "success" {
						if stdout != "built "+name+"\n" {
							t.Fatalf("build success stdout changed: %q", stdout)
						}
						built, err := os.ReadFile(filepath.Join(v[len(v)-1], name))
						if err != nil || !bytes.Equal(built, archiveData) {
							t.Fatal("diagnostics changed archive content")
						}
					} else if kind == "output-parent" {
						children, err := os.ReadDir(v[len(v)-1])
						if err != nil || len(children) != 0 {
							t.Fatal("preexisting build output changed")
						}
					} else {
						assertPackageAbsent(t, v[len(v)-1])
					}
				})
			}
		})
		prefix := "native package stage: arguments\nnative package stage: target\nnative package stage: archive-read\nnative package stage: checksums-read\nnative package stage: checksum\n"
		for _, kind := range []string{"archive-read", "checksums-read", "checksum", "archive-decode", "content", "extract", "success"} {
			t.Run(kind, func(t *testing.T) {
				v := append([]string(nil), verifyArgs...)
				v[len(v)-1] = filepath.Join(t.TempDir(), "SENSITIVE new-extract")
				want := prefix
				switch kind {
				case "archive-read":
					v[2] = filepath.Join(t.TempDir(), name)
					want = "native package stage: arguments\nnative package stage: target\nnative package stage: archive-read\n"
				case "checksums-read":
					v[4] = filepath.Join(t.TempDir(), "SENSITIVE-missing")
					want = strings.TrimSuffix(prefix, "native package stage: checksum\n")
				case "checksum":
					v[4] = filepath.Join(t.TempDir(), "SENSITIVE-checksums")
					if os.WriteFile(v[4], []byte("SENSITIVE invalid checksum"), 0600) != nil {
						t.Fatal("diagnostic checksum fixture")
					}
				case "archive-decode", "content":
					changed := append([]packageEntry(nil), entries...)
					for i := range changed {
						if changed[i].name == "manifest.json" {
							changed[i].data = []byte(`{"SENSITIVE":"invalid manifest"}`)
						}
					}
					data := packageWrite(t, changed, runtime.GOOS == "windows")
					if kind == "archive-decode" {
						data = []byte("SENSITIVE invalid archive")
					}
					sum := sha256.Sum256(data)
					dir := t.TempDir()
					v[2], v[4] = filepath.Join(dir, name), filepath.Join(dir, "SENSITIVE-checksums")
					if os.WriteFile(v[2], data, 0600) != nil || os.WriteFile(v[4], []byte(hex.EncodeToString(sum[:])+"  "+name+"\n"), 0600) != nil {
						t.Fatal("diagnostic content fixture")
					}
					want += "native package stage: archive-decode\n"
					if kind == "content" {
						want += "native package stage: content\n"
					}
				case "extract":
					v[len(v)-1] = filepath.Clean(t.TempDir())
					want += "native package stage: archive-decode\nnative package stage: content\nnative package stage: extract-root\nnative package stage: directory-parent\n"
					want += "native package parent: stage=leaf-missing category=exists\n"
				}
				stdout, stderr := run(t, kind == "success", v...)
				if kind != "success" {
					if stderr != want+"native package rejected\n" {
						t.Fatalf("wrong refusal stage: %q", stderr)
					}
					if kind != "extract" {
						assertPackageAbsent(t, v[len(v)-1])
					}
					return
				}
				if stdout != "verified agent-task-notify 0.2.0-rc.1 "+platform+" — doctor and six dry previews passed\n" {
					t.Fatalf("diagnostic success output: %q", stdout)
				}
				allowed := strings.Fields("arguments target archive-read checksums-read checksum archive-decode content extract-root directory-parent directory-create extract-directory extract-write private-readback mode hostfile extract-recheck isolated-random isolated-root isolated-directory isolated-version isolated-version-output isolated-doctor isolated-doctor-output isolated-preview isolated-preview-output isolated-state final-recheck complete")
				seen := map[string]int{}
				lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
				if len(lines) > 160 || !strings.HasPrefix(stderr, prefix) || !strings.HasSuffix(stderr, "native package stage: complete\n") {
					t.Fatalf("missing or unbounded stage sequence: %q", stderr)
				}
				for _, line := range lines {
					label, ok := strings.CutPrefix(line, "native package stage: ")
					if !ok || !slices.Contains(allowed, label) {
						t.Fatalf("unapproved diagnostic output: %q", line)
					}
					seen[label]++
				}
				for _, label := range allowed {
					if seen[label] == 0 {
						t.Fatalf("missing successful boundary %s", label)
					}
				}
				if seen["isolated-preview"] != 6 || seen["isolated-preview-output"] != 6 {
					t.Fatal("six preview boundaries missing")
				}
			})
		}
	})
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
		packagedRoot := filepath.ToSlash(filepath.Join(extract, "workbuddy"))
		cases := []string{"primary", "fallback", "primary-wins", "no-root", "relative-root", "missing-launcher", "script-startup-failure", "script-syntax-failure"}
		if runtime.GOOS == "windows" {
			cases = append(cases, "windows-native-primary", "windows-native-fallback", "windows-native-primary-wins")
		}
		for _, event := range []string{"UserPromptSubmit", "Stop"} {
			for _, name := range cases {
				t.Run(event+"/"+name, func(t *testing.T) {
					dir := t.TempDir()
					primary, fallback, wantRun := packagedRoot, "", true
					switch name {
					case "fallback":
						primary, fallback = "", packagedRoot
					case "windows-native-primary":
						primary = filepath.Join(extract, "workbuddy")
					case "windows-native-fallback":
						primary, fallback = "", filepath.Join(extract, "workbuddy")
					case "windows-native-primary-wins":
						primary, fallback, wantRun = filepath.Join(dir, "missing"), filepath.Join(extract, "workbuddy"), false
					case "primary-wins":
						primary, fallback, wantRun = filepath.ToSlash(filepath.Join(dir, "missing")), packagedRoot, false
					case "no-root":
						primary, wantRun = "", false
					case "relative-root":
						primary, fallback, wantRun = "relative", packagedRoot, false
					case "missing-launcher":
						primary, wantRun = filepath.ToSlash(dir), false
					case "script-startup-failure":
						primary, wantRun = filepath.ToSlash(dir), false
						if os.Mkdir(filepath.Join(dir, "hooks"), 0700) != nil || os.WriteFile(filepath.Join(dir, "hooks", "launch.sh"), []byte("printf 'synthetic partial output\\n'\nprintf 'synthetic launch error\\n' >&2\nexit 73\n"), 0600) != nil {
							t.Fatal("failed-launch fixture")
						}
					case "script-syntax-failure":
						primary, wantRun = filepath.ToSlash(dir), false
						if os.Mkdir(filepath.Join(dir, "hooks"), 0700) != nil || os.WriteFile(filepath.Join(dir, "hooks", "launch.sh"), []byte("if then\n"), 0600) != nil {
							t.Fatal("invalid-script fixture")
						}
					}
					// Every case uses the actual packaged hook command, including
					// invalid roots that fail before launch.sh can check anything.
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, bash, "-c", commands[event])
					cmd.Dir = dir
					cmd.Env = append(withoutEnvironmentNames(os.Environ(), "CODEBUDDY_PLUGIN_ROOT", "CLAUDE_PLUGIN_ROOT", "ATN_DATA_DIRECTORY"), "CODEBUDDY_PLUGIN_ROOT="+primary, "CLAUDE_PLUGIN_ROOT="+fallback, "ATN_DATA_DIRECTORY="+filepath.Join(dir, "unused-data"))
					cmd.Stdin = strings.NewReader("[")
					var output, stderr bytes.Buffer
					cmd.Stdout = &output
					cmd.Stderr = &stderr
					if err := cmd.Run(); err != nil || output.String() != "{\"continue\":true}\n" || stderr.Len() != 0 {
						t.Fatalf("packaged %s/%s: error=%v stdout=%q stderr=%q", event, name, err, output.String(), stderr.String())
					}
					if wantRun {
						// Invalid stdin proves that the archived native binary ran,
						// not just the command layer's neutral failure fallback.
						data, err := os.ReadFile(filepath.Join(dir, "unused-data", "input-diagnostics.json"))
						var observed struct {
							Count int `json:"count"`
						}
						if err != nil || json.Unmarshal(data, &observed) != nil || observed.Count != 1 {
							t.Fatal("WorkBuddy did not run archived binary with original stdin")
						}
					} else if _, err := os.Lstat(filepath.Join(dir, "unused-data")); !os.IsNotExist(err) {
						t.Fatal("invalid primary ran a fallback or created state")
					}
				})
			}
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
