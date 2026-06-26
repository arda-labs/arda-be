package scaffold

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/arda-labs/arda/tools/arda-cli/internal/filesystem"
	tpl "github.com/arda-labs/arda/tools/arda-cli/internal/template"
)

// MigrationData is the context passed to the migration template.
type MigrationData struct {
	ServiceName   string // e.g. "iam-service"
	MigrationName string // e.g. "create_user_table"
}

// CreateMigration generates a new SQL migration file inside apps/<service>/migrations/.
func CreateMigration(serviceName, migrationName string) error {
	dir := filepath.Join("apps", serviceName, "migrations")
	if err := filesystem.MkdirAll(dir); err != nil {
		return fmt.Errorf("create migration dir: %w", err)
	}

	base := fmt.Sprintf("%s_%s", time.Now().Format("20060102150405"), migrationName)
	fileName := base + ".sql"
	filePath := filepath.Join(dir, fileName)

	// Avoid collisions if multiple migrations are created in the same second.
	for i := 1; filesystem.Exists(filePath); i++ {
		fileName = fmt.Sprintf("%s_%03d.sql", base, i)
		filePath = filepath.Join(dir, fileName)
	}

	tplStr, err := tpl.Load(tpl.TplMigration)
	if err != nil {
		return err
	}

	engine := tpl.New(nil)
	data := MigrationData{
		ServiceName:   serviceName,
		MigrationName: migrationName,
	}

	rendered, err := engine.Render(tplStr, data)
	if err != nil {
		return fmt.Errorf("render migration template: %w", err)
	}

	if err := filesystem.WriteFile(filePath, []byte(rendered)); err != nil {
		return fmt.Errorf("write migration: %w", err)
	}

	fmt.Println("Created migration:", filePath)
	return nil
}
