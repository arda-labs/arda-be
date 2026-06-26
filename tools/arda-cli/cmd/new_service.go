package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/arda-labs/arda/tools/arda-cli/internal/scaffold"
	"github.com/arda-labs/arda/tools/arda-cli/internal/workspace"
	"github.com/spf13/cobra"
)

func NewServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "service <name>",
		Short: "Create new Go microservice",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serviceName := normalizeServiceName(args[0])
			if serviceName == "" {
				return fmt.Errorf("service name cannot be empty")
			}
			servicePath := filepath.Join("apps", serviceName)

			if err := scaffold.CreateService(servicePath, serviceName); err != nil {
				return err
			}

			if err := workspace.AddUse("./" + filepath.ToSlash(servicePath)); err != nil {
				return err
			}

			fmt.Println("Created service:", serviceName)
			return nil
		},
	}
}

func normalizeServiceName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return name
	}
	for _, suffix := range []string{"-service", "-gateway", "-adapter"} {
		if strings.HasSuffix(name, suffix) {
			return name
		}
	}
	return name + "-service"
}
