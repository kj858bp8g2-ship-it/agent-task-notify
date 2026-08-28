package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kj858bp8g2-ship-it/agent-task-notify/internal/configuration"
	"golang.org/x/sys/windows"
)

func TestWindowsShortAliasesCannotPutHostInsidePackageOrData(t *testing.T) {
	for _, kind := range []string{"package", "data"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t, "cursor", nil)
			root := privateDirectory(t, filepath.Join(f.root, "long synthetic "+kind+" directory"))
			if kind == "package" {
				f.options.PackageRoot = root
				f.options.Executable = filepath.Join(root, "agent-task-notify.exe")
				put(t, f.options.Executable, []byte("synthetic-not-executed"))
			} else {
				var err error
				f.repo, err = configuration.Open(root, f.options.PackageRoot)
				if err != nil {
					t.Fatal(err)
				}
			}
			name, err := windows.UTF16PtrFromString(root)
			if err != nil {
				t.Fatal(err)
			}
			buffer := make([]uint16, 32768)
			size, err := windows.GetShortPathName(name, &buffer[0], uint32(len(buffer)))
			if err != nil || size == 0 || size >= uint32(len(buffer)) {
				t.Fatal("synthetic short-path lookup failed", err)
			}
			alias := windows.UTF16ToString(buffer[:size])
			// A volume that has no short names is not evidence that the alias
			// rejection was exercised. Never enable 8.3 or alter volume policy.
			if strings.EqualFold(alias, root) {
				t.Skip("test volume supplies no distinct 8.3 alias")
			}
			original, err := os.Lstat(root)
			if err != nil {
				t.Fatal(err)
			}
			aliased, err := os.Lstat(alias)
			if err != nil || !os.SameFile(original, original) || !os.SameFile(aliased, aliased) || !os.SameFile(original, aliased) {
				t.Fatal("distinct alias does not identify synthetic root")
			}
			f.options.ConfigPath = filepath.Join(alias, "hooks.json")
			p, err := PlanInstall(context.Background(), f.repo, f.options)
			if err == nil {
				t.Fatal("alias allowed host inside " + kind)
			}
			if ApplyInstall(context.Background(), f.repo, p) == nil {
				t.Fatal("alias refusal plan authorized mutation")
			}
			if _, err := os.Lstat(filepath.Join(root, "hooks.json")); !os.IsNotExist(err) {
				t.Fatal("alias preview wrote excluded target")
			}
		})
	}
}
