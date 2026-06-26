package scaffold

import (
	"fmt"
	"path/filepath"

	"github.com/arda-labs/arda/tools/arda-cli/internal/filesystem"
	tpl "github.com/arda-labs/arda/tools/arda-cli/internal/template"
)

// ServiceData is the context passed to service templates.
type ServiceData struct {
	ServiceName      string // e.g. "iam-service"
	ServiceShortName string // e.g. "iam"
	ModulePath       string // e.g. "github.com/arda-labs/arda/apps/iam-service"
	GoVersion        string // e.g. "1.26"
}

// CreateService generates a new microservice skeleton under apps/.
func CreateService(servicePath, serviceName string) error {
	if filesystem.Exists(servicePath) {
		return fmt.Errorf("service already exists: %s", servicePath)
	}

	engine := tpl.New(nil)
	shortName := serviceName[:len(serviceName)-len("-service")]

	data := ServiceData{
		ServiceName:      serviceName,
		ServiceShortName: shortName,
		ModulePath:       fmt.Sprintf("github.com/arda-labs/arda/apps/%s", serviceName),
		GoVersion:        "1.26",
	}

	dirs := []string{
		filepath.Join(servicePath, "cmd", serviceName),
		filepath.Join(servicePath, "configs"),
		filepath.Join(servicePath, "internal", "app"),
		filepath.Join(servicePath, "internal", "config"),
		filepath.Join(servicePath, "internal", "domain"),
		filepath.Join(servicePath, "internal", "handler"),
		filepath.Join(servicePath, "internal", "repository"),
		filepath.Join(servicePath, "internal", "service"),
		filepath.Join(servicePath, "internal", "transport", "http"),
		filepath.Join(servicePath, "migrations"),
		filepath.Join(servicePath, "deployments", "docker"),
		filepath.Join(servicePath, "deployments", "helm"),
		filepath.Join(servicePath, "deployments", "k8s"),
	}

	for _, dir := range dirs {
		if err := filesystem.MkdirAll(dir); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	entries := []struct {
		key    tpl.TemplateKey
		target string
	}{
		{key: tpl.TplGoMod, target: filepath.Join(servicePath, "go.mod")},
		{key: tpl.TplMainGo, target: filepath.Join(servicePath, "cmd", serviceName, "main.go")},
		{key: tpl.TplConfigYaml, target: filepath.Join(servicePath, "configs", "config.yaml")},
		{key: tpl.TplReadme, target: filepath.Join(servicePath, "README.md")},
		{key: tpl.TplDockerfile, target: filepath.Join(servicePath, "Dockerfile")},
		{key: tpl.TplMakefile, target: filepath.Join(servicePath, "Makefile")},
		{key: tpl.TplTaskfile, target: filepath.Join(servicePath, "Taskfile.yml")},
	}

	for _, e := range entries {
		tplStr, err := tpl.Load(e.key)
		if err != nil {
			return err
		}

		rendered, err := engine.Render(tplStr, data)
		if err != nil {
			return fmt.Errorf("render %s: %w", e.key, err)
		}

		if err := filesystem.WriteFile(e.target, []byte(rendered)); err != nil {
			return fmt.Errorf("write %s: %w", e.target, err)
		}
	}

	return nil
}
