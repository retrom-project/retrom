package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLaunchAndVariantHaveNoImportWorkflowDependencies(t *testing.T) {
	t.Parallel()
	graph := productionImportGraph(t)
	for name := range graph {
		if protectedRuntimePackage(name) {
			if chain := importWorkflowChain(graph, name, nil); chain != nil {
				t.Errorf("runtime production dependency: %s", strings.Join(chain, " -> "))
			}
		}
	}
}

func productionImportGraph(t *testing.T) map[string][]string {
	t.Helper()
	root := sourceRoot(t)
	graph := make(map[string][]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		name := "retrom/internal/" + filepath.ToSlash(relative)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, item := range file.Imports {
			dependency, err := strconv.Unquote(item.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(dependency, "retrom/internal/") {
				graph[name] = append(graph[name], dependency)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func protectedRuntimePackage(name string) bool {
	for _, prefix := range []string{
		"retrom/internal/launch", "retrom/internal/service/launch", "retrom/internal/service/gamevariant",
		"retrom/internal/persistence/launch", "retrom/internal/persistence/gamevariant",
		"retrom/internal/composition/launch", "retrom/internal/composition/gamevariant",
		"retrom/internal/service/saves", "retrom/internal/persistence/saves",
		"retrom/internal/service/isolation", "retrom/internal/persistence/isolation",
		"retrom/internal/service/runtimesession", "retrom/internal/persistence/runtimesession",
	} {
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}
	return false
}

func importWorkflowChain(graph map[string][]string, name string, seen map[string]bool) []string {
	if strings.Contains(name, "/libraryimport") || name == "retrom/internal/composition/importworkflow" {
		return []string{name}
	}
	if seen == nil {
		seen = make(map[string]bool)
	}
	if seen[name] {
		return nil
	}
	seen[name] = true
	for _, dependency := range graph[name] {
		if chain := importWorkflowChain(graph, dependency, seen); chain != nil {
			return append([]string{name}, chain...)
		}
	}
	return nil
}

func TestImportWorkflowBoundaryDetectsIndirectProductionDependency(t *testing.T) {
	graph := map[string][]string{
		"retrom/internal/service/launch": {"retrom/internal/content/arcade"},
		"retrom/internal/content/arcade": {"retrom/internal/service/libraryimport"},
	}
	chain := importWorkflowChain(graph, "retrom/internal/service/launch", nil)
	if len(chain) != 3 {
		t.Fatalf("indirect dependency escaped: %v", chain)
	}
	delete(graph, "retrom/internal/content/arcade")
	if chain := importWorkflowChain(graph, "retrom/internal/service/launch", nil); chain != nil {
		t.Fatalf("legal graph rejected: %v", chain)
	}
}
