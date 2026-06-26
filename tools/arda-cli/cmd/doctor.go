package cmd

import (
	"fmt"

	"github.com/arda-labs/arda/tools/arda-cli/internal/exec"
	"github.com/spf13/cobra"
)

func DoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check required tools",
		Run: func(cmd *cobra.Command, args []string) {
			for _, tool := range []string{
				"go", "git", "docker", "kubectl",
				"helm", "protoc", "buf", "wire", "kratos",
			} {
				checkTool(tool)
			}
		},
	}
}

func checkTool(name string) {
	path, err := exec.LookPath(name)
	if err != nil {
		fmt.Printf("✘ %-10s not found\n", name)
		return
	}
	fmt.Printf("✔ %-10s %s\n", name, path)
}
