package workspace

import (
	"github.com/arda-labs/arda/tools/arda-cli/internal/exec"
)

// AddUse runs "go work use <path>" to add a module to the workspace.
func AddUse(path string) error {
	return exec.Command("go", "work", "use", path)
}
