package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags, or falls back to dev.
var Version = "0.1.0"

func Execute() {
	rootCmd := &cobra.Command{
		Use:   "arda",
		Short: "Arda internal developer CLI",
		Long:  "Arda CLI — scaffold, manage, and develop Go microservices in the Arda monorepo.\n\nCommands:\n  doctor                 Check required tools\n  new service <name>     Create a new microservice\n  new migration <s> <n>  Create a new SQL migration\n  version                Show CLI version",
	}

	rootCmd.AddCommand(DoctorCmd())
	rootCmd.AddCommand(NewCmd())
	rootCmd.AddCommand(VersionCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}