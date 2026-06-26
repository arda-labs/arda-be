package exec

import (
	"os"
	"os/exec"
)

// Command runs an external command, streaming output to stdout/stderr.
func Command(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// LookPath searches for an executable in PATH.
func LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
