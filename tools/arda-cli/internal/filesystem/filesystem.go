package filesystem

import (
	"os"
	"path/filepath"
)

// Exists reports whether the path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// MkdirAll creates a directory and all its parents.
func MkdirAll(path string) error {
	return os.MkdirAll(path, 0755)
}

// WriteFile writes data to a file, creating parent directories if needed.
func WriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := MkdirAll(dir); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
