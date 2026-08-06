package store

import (
	"fmt"
	"io/fs"
	"os"
)

func mkdirAll(path string, mode fs.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return nil
}
