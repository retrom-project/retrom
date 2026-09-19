package netplayprofile

import (
	"fmt"
	"os"
	"path/filepath"

	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/model/netplayprofile"
)

const (
	ManifestRelativePath   = "netplay/v2/manifest.json"
	ManifestSchemaRelative = "netplay/v2/schema.json"
)

func LoadRegistry(dependencyRoot string, dependencySet *dependencies.Set) (*netplayprofile.Registry, error) {
	contents, err := os.ReadFile(filepath.Join(dependencyRoot, ManifestRelativePath))
	if err != nil {
		return nil, fmt.Errorf("%w: manifest unavailable", netplayprofile.ErrManifestInvalid)
	}
	if _, err := os.Stat(filepath.Join(dependencyRoot, ManifestSchemaRelative)); err != nil {
		return nil, fmt.Errorf("%w: schema unavailable", netplayprofile.ErrManifestInvalid)
	}
	if dependencySet == nil {
		return nil, fmt.Errorf("%w: dependencies unavailable", netplayprofile.ErrManifestInvalid)
	}
	registry, err := netplayprofile.ParseRegistry(contents, dependencySet.RuntimeCatalog.Bindings)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return registry, nil
}
