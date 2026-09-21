package netplay

import (
	"os"
	"path/filepath"

	"retrom/internal/dependencies"
	"retrom/internal/netplay/profile"
	"retrom/internal/runtimecatalog"
)

func parseRegistry(raw []byte, set *dependencies.Set) (*Registry, error) {
	return profile.ParseRegistry(raw, set)
}

func fixtureDependencySet() *dependencies.Set {
	contents, err := os.ReadFile(filepath.Join("..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		panic(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(contents)
	if err != nil {
		panic(err)
	}
	return &dependencies.Set{RuntimeCatalog: catalog}
}
