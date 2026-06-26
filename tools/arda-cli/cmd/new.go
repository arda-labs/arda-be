package cmd

import "github.com/spf13/cobra"

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create Arda resources",
	}

	cmd.AddCommand(NewServiceCmd())
	cmd.AddCommand(NewMigrationCmd())

	return cmd
}