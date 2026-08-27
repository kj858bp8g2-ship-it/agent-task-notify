// Package configuration stores one installation-scoped protected transaction.
package configuration

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/store"
)

type Repository struct{ directory, packageRoot string }

// Open only resolves and validates paths; it never creates a data directory.
func Open(explicitDirectory, packageRoot string) (*Repository, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, errConfigurationUnavailable
	}
	exeRoot := filepath.Dir(exe)
	if packageRoot == "" {
		packageRoot = exeRoot
	}
	if !validDirectoryPath(packageRoot) {
		return nil, errConfigurationUnavailable
	}
	info, err := os.Lstat(packageRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errConfigurationUnavailable
	}
	directory := explicitDirectory
	if directory == "" {
		directory = os.Getenv("ATN_DATA_DIRECTORY")
	}
	if directory == "" {
		base := os.Getenv("LOCALAPPDATA")
		if runtime.GOOS == "darwin" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, errConfigurationUnavailable
			}
			base = filepath.Join(home, "Library", "Application Support")
		}
		if !validDirectoryPath(base) {
			return nil, errConfigurationUnavailable
		}
		info, err := os.Lstat(base)
		if err != nil || !info.IsDir() {
			return nil, errConfigurationUnavailable
		}
		directory = filepath.Join(base, "AgentTaskNotifyNative")
	}
	r := &Repository{directory: directory, packageRoot: packageRoot}
	if !validDirectoryPath(directory) || within(directory, packageRoot) || within(directory, exeRoot) || r.checkDirectory() != nil {
		return nil, errConfigurationUnavailable
	}
	return r, nil
}

func (r *Repository) Directory() string {
	if r == nil {
		return ""
	}
	return r.directory
}

func (r *Repository) checkDirectory() error {
	if r == nil || !validDirectoryPath(r.directory) {
		return errConfigurationUnavailable
	}
	home, err := os.UserHomeDir()
	if err != nil || samePath(r.directory, home) || filepath.Dir(r.directory) == r.directory || within(r.directory, r.packageRoot) {
		return errConfigurationUnavailable
	}
	components := strings.Split(filepath.ToSlash(r.directory), "/")
	for i, component := range components {
		if strings.EqualFold(component, "AgentTaskNotify") || (i > 0 && strings.EqualFold(components[i-1], ".codex") && strings.EqualFold(component, "long-task-notify")) {
			return errConfigurationUnavailable
		}
	}
	if info, err := os.Lstat(r.directory); os.IsNotExist(err) {
		if store.CheckPrivateDirectoryParent(r.directory) != nil {
			return errConfigurationUnavailable
		}
	} else if err != nil || !info.IsDir() || store.CheckPrivateDirectory(r.directory) != nil {
		return errConfigurationUnavailable
	}
	// Local, metadata-only source detection: no directory enumeration or marker
	// contents. This deliberately rejects any Git ancestor, not just this repo.
	for current := r.directory; ; current = filepath.Dir(current) {
		if hasSourceMarker(current) {
			return errConfigurationUnavailable
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

func hasSourceMarker(directory string) bool {
	if _, err := os.Lstat(filepath.Join(directory, ".git")); !os.IsNotExist(err) {
		return true
	}
	mod, err := os.Lstat(filepath.Join(directory, "go.mod"))
	if os.IsNotExist(err) {
		return false
	}
	if err != nil || !mod.Mode().IsRegular() {
		return true
	}
	config, err := os.Lstat(filepath.Join(directory, "config"))
	if os.IsNotExist(err) {
		return false
	}
	if err != nil || !config.IsDir() || config.Mode()&os.ModeSymlink != 0 {
		return true
	}
	_, err = os.Lstat(filepath.Join(directory, "config", "native-source-files.json"))
	return !os.IsNotExist(err)
}

func validDirectoryPath(path string) bool {
	if !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		// Reject device/UNC namespaces, streams and Win32 normalization aliases.
		if strings.HasPrefix(path, `\\`) || len(filepath.VolumeName(path)) != 2 {
			return false
		}
		for _, component := range strings.Split(filepath.ToSlash(strings.TrimPrefix(path, filepath.VolumeName(path))), "/") {
			if strings.ContainsAny(component, `<>:"|?*`) || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
				return false
			}
			name := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
			if name == "CON" || name == "PRN" || name == "AUX" || name == "NUL" || (len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) && name[3] >= '1' && name[3] <= '9') {
				return false
			}
		}
	}
	return true
}

func samePath(a, b string) bool { return strings.EqualFold(a, b) }
func within(path, root string) bool {
	return root != "" && (samePath(path, root) || strings.HasPrefix(strings.ToLower(path), strings.ToLower(strings.TrimRight(root, string(filepath.Separator))+string(filepath.Separator))))
}

// Prepare creates only this root and the fixed private leaf directories.
func (r *Repository) Prepare() error {
	if r.checkDirectory() != nil {
		return errConfigurationUnavailable
	}
	if store.EnsurePrivateDirectory(r.directory) != nil {
		return errConfigurationUnavailable
	}
	for _, name := range []string{"sessions", "runs", "jobs", "locks", "receipts", "backups"} {
		if store.EnsurePrivateDirectory(filepath.Join(r.directory, name)) != nil {
			return errConfigurationUnavailable
		}
	}
	return nil
}
