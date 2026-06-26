package cmd

import (
	"fmt"
	"strings"

	"github.com/arda-labs/arda/tools/arda-cli/internal/scaffold"
	"github.com/spf13/cobra"
)

func NewMigrationCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migration <service> <name>",
		Short: "Create new SQL migration",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := normalizeServiceName(args[0])
			if serviceName == "" {
				return fmt.Errorf("invalid service name")
			}
			migrationName := normalizeMigrationName(args[1])
			if migrationName == "" {
				return fmt.Errorf("invalid migration name")
			}

			return scaffold.CreateMigration(serviceName, migrationName)
		},
	}
}

func normalizeMigrationName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}
