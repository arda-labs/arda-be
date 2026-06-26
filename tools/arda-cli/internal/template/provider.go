package template

import (
	"embed"
	"fmt"
)

//go:embed templates/service/*.tpl
//go:embed templates/service/cmd/*.tpl
//go:embed templates/service/configs/*.tpl
//go:embed templates/migration/*.tpl
var templateFS embed.FS

// TemplateKey identifies a template file path relative to templates/.
type TemplateKey string

const (
	TplGoMod       TemplateKey = "service/go.mod.tpl"
	TplReadme      TemplateKey = "service/README.md.tpl"
	TplMainGo      TemplateKey = "service/cmd/main.go.tpl"
	TplConfigYaml  TemplateKey = "service/configs/config.yaml.tpl"
	TplDockerfile  TemplateKey = "service/Dockerfile.tpl"
	TplMakefile    TemplateKey = "service/Makefile.tpl"
	TplTaskfile    TemplateKey = "service/Taskfile.yml.tpl"
	TplMigration   TemplateKey = "migration/migration.sql.tpl"
)

// Load returns the content of the given embedded template.
func Load(key TemplateKey) (string, error) {
	data, err := templateFS.ReadFile("templates/" + string(key))
	if err != nil {
		return "", fmt.Errorf("load template %s: %w", key, err)
	}
	return string(data), nil
}
