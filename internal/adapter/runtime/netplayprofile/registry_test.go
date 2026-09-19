package netplayprofile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/runtime/runtimecatalog"
	modelprofile "retrom/internal/model/netplayprofile"
)

func TestLoadRegistryRetainsFailurePrecedence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, manifest, schema, reason string
		nilSet                         bool
	}{
		{"missing manifest wins", "", "", "manifest unavailable", true},
		{"missing schema precedes decode", "{", "", "schema unavailable", false},
		{"missing schema precedes dependency", "{}", "", "schema unavailable", true},
		{"dependency precedes decode", "{", "not JSON", "dependencies unavailable", true},
		{"decode with present dependencies", "{", "not JSON", "schema", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.manifest != "" {
				writeRegistryFile(t, root, ManifestRelativePath, []byte(test.manifest))
			}
			if test.schema != "" {
				writeRegistryFile(t, root, ManifestSchemaRelative, []byte(test.schema))
			}
			set := &dependencies.Set{}
			if test.nilSet {
				set = nil
			}
			registry, err := LoadRegistry(root, set)
			var pathError *os.PathError
			if registry != nil || !errors.Is(err, modelprofile.ErrManifestInvalid) ||
				err.Error() != "NETPLAY_MANIFEST_INVALID: "+test.reason || errors.As(err, &pathError) {
				t.Fatalf("load = %+v, %v; want private-path-free %s", registry, err, test.reason)
			}
		})
	}
}

func TestLoadRegistryUsesManifestBytesAndSchemaExistence(t *testing.T) {
	t.Parallel()
	dataRoot := filepath.Join("..", "..", "..", "..", "data")
	raw, err := os.ReadFile(filepath.Join(dataRoot, ManifestRelativePath))
	if err != nil {
		t.Fatal(err)
	}
	catalogRaw, err := os.ReadFile(filepath.Join(dataRoot, "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(catalogRaw)
	if err != nil {
		t.Fatal(err)
	}
	for _, schemaDirectory := range []bool{false, true} {
		root := t.TempDir()
		writeRegistryFile(t, root, ManifestRelativePath, raw)
		if schemaDirectory {
			if err := os.MkdirAll(filepath.Join(root, ManifestSchemaRelative), 0o700); err != nil {
				t.Fatal(err)
			}
		} else {
			writeRegistryFile(t, root, ManifestSchemaRelative, []byte("present; deliberately not schema JSON"))
		}
		registry, err := LoadRegistry(root, &dependencies.Set{RuntimeCatalog: catalog})
		if err != nil {
			t.Fatal(err)
		}
		if registry.ManifestDigest != "53da8930431aaf5c8354f36b3b99194a972a00310fc44c2744ea86ddaad08fa9" ||
			len(registry.Profiles()) != 8 {
			t.Fatalf("installed registry = %+v", registry)
		}
		if _, err := LoadRegistry(root, &dependencies.Set{}); !errors.Is(err, modelprofile.ErrManifestInvalid) ||
			err.Error() != "NETPLAY_MANIFEST_INVALID: profile" {
			t.Fatalf("empty bindings changed dependency/profile error partition: %v", err)
		}
	}
}

func writeRegistryFile(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
