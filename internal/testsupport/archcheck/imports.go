package archcheck

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// AssertBusinessImports checks production imports in the caller's package directory.
func AssertBusinessImports(t testing.TB) {
	AssertBusinessImportsIn(t, ".")
}

// AssertBusinessImportsIn checks a directory for repository-wide boundary tests.
func AssertBusinessImportsIn(t testing.TB, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		name := filepath.Join(directory, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, dependency := range file.Imports {
			value, err := strconv.Unquote(dependency.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if forbiddenBusinessImport(value) {
				t.Errorf("%s imports forbidden dependency %s; depend on a business port", name, value)
			}
		}
	}
}

// AssertNoSourceTokens checks only production Go files in the caller's package.
func AssertNoSourceTokens(t testing.TB, forbidden []string) {
	t.Helper()
	for _, source := range productionSources(t) {
		for _, token := range forbidden {
			if strings.Contains(source.contents, token) {
				t.Errorf("%s contains forbidden token %q", source.name, token)
			}
		}
	}
}

// CountSourceToken counts occurrences only in the caller's production Go files.
func CountSourceToken(t testing.TB, token string) int {
	t.Helper()
	count := 0
	for _, source := range productionSources(t) {
		count += strings.Count(source.contents, token)
	}
	return count
}

type sourceFile struct {
	name     string
	contents string
}

func productionSources(t testing.TB) []sourceFile {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	result := make([]sourceFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		contents, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, sourceFile{name: entry.Name(), contents: string(contents)})
	}
	return result
}

func forbiddenBusinessImport(value string) bool {
	for _, prefix := range []string{
		"database/sql", "github.com/jackc/pgx", "retrom/internal/persistence",
		"retrom/internal/database", "retrom/internal/store",
		"retrom/internal/application", "retrom/internal/composition", "retrom/internal/httpapi", "retrom/cmd",
	} {
		if value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}
