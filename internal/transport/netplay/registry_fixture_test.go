package netplay

import (
	"os"
	"path/filepath"

	"retrom/internal/capability/runtime/runtimecatalog"
)

func fixtureBindings() []runtimecatalog.Binding {
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "data", "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		panic(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(contents)
	if err != nil {
		panic(err)
	}
	return catalog.Bindings
}
