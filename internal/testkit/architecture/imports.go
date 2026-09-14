// Package architecture contains reusable tests for Retrom's package boundaries.
package architecture

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// PackageDirectory returns the directory containing the calling test file.
// It keeps architecture tests colocated with the package they inspect instead
// of making them depend on a working-directory-relative path.
func PackageDirectory(t testing.TB) string {
	t.Helper()
	return packageDirectory(t)
}

func packageDirectory(t testing.TB) string {
	_, filename, _, ok := runtime.Caller(2)
	if !ok {
		t.Fatal("architecture: locate calling test")
	}
	return filepath.Dir(filename)
}

// AssertBusinessImports checks the production files in the calling package.
func AssertBusinessImports(t testing.TB) {
	t.Helper()
	AssertBusinessImportsIn(t, packageDirectory(t))
}

// AssertBusinessImportsIn checks one package directory for direct database
// implementation imports.
func AssertBusinessImportsIn(t testing.TB, directory string) {
	t.Helper()
	files, err := GoSourceFilesIn(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, dependency := range file.Imports {
			value, err := strconv.Unquote(dependency.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if databaseDependency(value) {
				t.Errorf("%s imports database implementation %s; depend on a business port", name, value)
			}
		}
	}
}

// AssertNoImportsIn rejects a dependency prefix from production files below a
// directory. It is useful for enforcing one-way layer boundaries without
// coupling the test to the package's working directory.
func AssertNoImportsIn(t testing.TB, directory, forbiddenPrefix string) {
	t.Helper()
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse imports in %q: %w", path, err)
		}
		for _, dependency := range file.Imports {
			value, err := strconv.Unquote(dependency.Path.Value)
			if err != nil {
				return fmt.Errorf("decode import path in %q: %w", path, err)
			}
			if value == forbiddenPrefix || strings.HasPrefix(value, forbiddenPrefix) {
				t.Errorf("%s imports forbidden layer %s", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// GoSourceFiles returns the production Go files in the calling package.
func GoSourceFiles(t testing.TB) ([]string, error) {
	t.Helper()
	return GoSourceFilesIn(packageDirectory(t))
}

// GoSourceFilesIn returns the production Go files in one package directory.
func GoSourceFilesIn(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read Go package directory %q: %w", directory, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		files = append(files, filepath.Join(directory, entry.Name()))
	}
	return files, nil
}

// AssertBusinessImportsTree checks every package below the calling layer
// directory. It is used only by layer-wide tests.
func AssertBusinessImportsTree(t testing.TB) {
	t.Helper()
	err := filepath.WalkDir(packageDirectory(t), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			AssertBusinessImportsIn(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// AssertNoProviderAuthority rejects Provider-owned implementation details from
// the production files in the calling package.
func AssertNoProviderAuthority(t testing.TB) {
	t.Helper()
	assertNoTokens(t, packageDirectory(t), []string{
		"selected_core_" + "artifacts",
		"adapter_" + "abi",
		"runtime_" + "family",
		"route_" + "key",
		"RetromRuntime" + "File",
		"RPGMaker" + "Version",
	}, "retains Provider-owned token")
}

// AssertNoLegacyLaunchAuthority rejects the old launch authority fields from
// the production files in the calling package.
func AssertNoLegacyLaunchAuthority(t testing.TB) {
	t.Helper()
	assertNoTokens(t, packageDirectory(t), []string{
		"runtime_family", "runtimeFamily", "route_key", "routeKey", "core_artifacts",
		"core_artifact_id", "CoreArtifactID", "adapterAbi", "saveAbi", "payloadKind",
		"nativeProfile", "resumeSlot",
	}, "retains legacy launch authority")
}

// AssertExactlyOneRuntimeBuilderUse verifies the generic runtime envelope is
// built once in the calling adapter package.
func AssertExactlyOneRuntimeBuilderUse(t testing.TB) {
	t.Helper()
	files, err := GoSourceFilesIn(packageDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	uses := 0
	for _, name := range files {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		uses += strings.Count(string(contents), "runtimeBuilder.Build(")
	}
	if uses != 1 {
		t.Fatalf("runtimeBuilder.Build call count = %d, want one generic Envelope path", uses)
	}
}

// AssertOpaqueProviderCheckpoints rejects legacy checkpoint authority fields
// from the production files in the calling package.
func AssertOpaqueProviderCheckpoints(t testing.TB) {
	t.Helper()
	assertNoTokens(t, packageDirectory(t), []string{
		"core_artifact", "runtime_family", "route_key", "adapter_abi", "save_abi",
		"payload_kind", "native_profile", "resume_slot", "rpgmaker/checkpoint",
	}, "still contains legacy checkpoint authority")
}

func assertNoTokens(t testing.TB, directory string, tokens []string, message string) {
	t.Helper()
	files, err := GoSourceFilesIn(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range tokens {
			if strings.Contains(string(contents), token) {
				t.Errorf("%s %q", name, message+" "+token)
			}
		}
	}
}

func databaseDependency(value string) bool {
	for _, prefix := range []string{
		"database/sql", "modernc.org/sqlite", "retrom/internal/repo",
	} {
		if value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}
