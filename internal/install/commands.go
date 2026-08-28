package install

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func cleanAbsolute(path string) bool {
	return utf8.ValidString(path) && filepath.IsAbs(path) && filepath.Clean(path) == path && strings.IndexFunc(path, unicode.IsControl) < 0
}

// Shell selection belongs to the installing host/user, not our current shell.
// Only fixed arguments are rendered; hook input never enters this string.
func renderCommand(shell, executable, agent, directory string) (string, error) {
	if !cleanAbsolute(executable) || !cleanAbsolute(directory) || !automaticAgent(agent) {
		return "", ErrInvalid
	}
	args := []string{executable, "hook", "--agent", agent, "--data-directory", directory}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	prefix := ""
	switch shell {
	case "posix":
		args[0] = filepath.ToSlash(executable)
	case "powershell":
		prefix = "& "
		quote = func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	case "cmd":
		for _, s := range args {
			if strings.ContainsAny(s, "%!\"&|^<>()`") {
				return "", ErrInvalid
			}
		}
		quote = func(s string) string { return "\"" + s + "\"" }
	default:
		return "", ErrShellRequired
	}
	for i, s := range args {
		args[i] = quote(s)
	}
	return prefix + strings.Join(args, " "), nil
}

func renderShim(executable, packageRoot, directory string) ([]byte, error) {
	if !cleanAbsolute(executable) || !cleanAbsolute(packageRoot) || !cleanAbsolute(directory) {
		return nil, ErrInvalid
	}
	path := filepath.ToSlash(filepath.Join(packageRoot, "integrations", "opencode", "bridge.mjs"))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	location := (&url.URL{Scheme: "file", Path: path}).String()
	bridge, _ := json.Marshal(location)
	exe, _ := json.Marshal(executable)
	data, _ := json.Marshal(directory)
	return []byte("import { createAgentTaskNotify } from " + string(bridge) + ";\nexport default createAgentTaskNotify({ executable: " + string(exe) + ", dataDirectory: " + string(data) + " });\n"), nil
}
