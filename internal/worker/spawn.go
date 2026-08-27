package worker

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

var errSpawn = errors.New("worker unavailable")

// SpawnWorker starts only the fixed worker protocol, never a shell command.
func SpawnWorker(executable, dataDirectory, jobKey string) error {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(dataDirectory) || len(jobKey) != 64 {
		return errSpawn
	}
	for _, c := range jobKey {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return errSpawn
		}
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return errSpawn
	}
	defer null.Close()
	cmd := exec.Command(executable, "worker", "--data-directory", dataDirectory, "--job", jobKey)
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	cmd.SysProcAttr = detachedAttributes()
	if cmd.Start() != nil {
		return errSpawn
	}
	if cmd.Process.Release() != nil {
		return errSpawn
	}
	return nil
}
