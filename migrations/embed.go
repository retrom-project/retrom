package migrations

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

//go:embed *.sql
var files embed.FS

var migrationName = regexp.MustCompile(`^([0-9]{3})_[a-z0-9_]+\.sql$`)

var errInvalidMigration = errors.New("invalid migration sequence")

type Migration struct {
	ID  string
	SQL string
}

// All is the single ordered migration list used by startup and schema tests.
func All() ([]Migration, error) {
	return readMigrations(files)
}

func readMigrations(source fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	result := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil || match[1] != fmt.Sprintf("%03d", len(result)+1) {
			return nil, fmt.Errorf("%w: expected migration %03d, got %s", errInvalidMigration, len(result)+1, entry.Name())
		}
		contents, readErr := fs.ReadFile(source, entry.Name())
		if readErr != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), readErr)
		}
		if strings.TrimSpace(string(contents)) == "" {
			return nil, fmt.Errorf("%w: empty migration: %s", errInvalidMigration, entry.Name())
		}
		result = append(result, Migration{ID: strings.TrimSuffix(entry.Name(), ".sql"), SQL: string(contents)})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: no numbered migrations", errInvalidMigration)
	}
	return result, nil
}
