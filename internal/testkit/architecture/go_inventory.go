package architecture

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

var ErrTypeAnalysis = errors.New("architecture: incomplete Go type analysis")

// GoPackageInventory records the actual resolved graph, including tagged tests.
type GoPackageInventory struct {
	ID         string        `json:"id"`
	ImportPath string        `json:"importPath"`
	Build      string        `json:"build"`
	Files      []string      `json:"files"`
	Imports    []string      `json:"imports"`
	Symbols    []GoSymbol    `json:"symbols"`
	References []GoReference `json:"references"`
}

// GoSymbol is a declaration resolved by go/types, not a name-pattern guess.
type GoSymbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Type     string `json:"type"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
}

// GoReference records a resolved identifier or method reference.
type GoReference struct {
	DefinitionFile string `json:"definitionFile"`
	DefinitionLine int    `json:"definitionLine"`
	Target         string `json:"target"`
	File           string `json:"file"`
	Line           int    `json:"line"`
	Column         int    `json:"column"`
}

// InspectGo builds both normal and integration graphs and fails on loading errors.
func InspectGo(ctx context.Context, root string, sources []string) ([]GoPackageInventory, error) {
	patterns := goInventoryPatterns(sources)
	if len(patterns) == 0 {
		return nil, ErrEmptySources
	}
	result := make([]GoPackageInventory, 0)
	for _, build := range []string{"default", "integration"} {
		graph, err := loadInventoryGraph(ctx, root, patterns, build)
		if err != nil {
			return nil, err
		}
		for _, pkg := range graph {
			item, err := inspectGoPackage(root, build, pkg)
			if err != nil {
				return nil, err
			}
			result = append(result, item)
		}
	}
	slices.SortFunc(result, func(left, right GoPackageInventory) int {
		return strings.Compare(left.Build+"\x00"+left.ID, right.Build+"\x00"+right.ID)
	})
	return result, nil
}

func goInventoryPatterns(sources []string) []string {
	patterns := make([]string, 0)
	for _, source := range sources {
		if strings.HasSuffix(source, ".go") {
			patterns = append(patterns, "./"+path.Dir(source))
		}
	}
	slices.Sort(patterns)
	return slices.Compact(patterns)
}

func loadInventoryGraph(
	ctx context.Context, root string, patterns []string, build string,
) ([]*packages.Package, error) {
	config := &packages.Config{
		Context: ctx, Dir: root, Tests: true, Fset: token.NewFileSet(),
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
	}
	if build == "integration" {
		config.BuildFlags = []string{"-tags=integration"}
	}
	graph, err := packages.Load(config, patterns...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTypeAnalysis, err)
	}
	issues := make([]error, 0)
	packages.Visit(graph, nil, func(pkg *packages.Package) {
		for _, issue := range pkg.Errors {
			issues = append(issues, fmt.Errorf("%w: %s: %s", ErrTypeAnalysis, pkg.PkgPath, issue.Msg))
		}
	})
	if len(issues) > 0 {
		return nil, errors.Join(issues...)
	}
	local := make([]*packages.Package, 0, len(graph))
	packages.Visit(graph, func(pkg *packages.Package) bool {
		if pkg.Module != nil && filepath.Clean(pkg.Module.Dir) == filepath.Clean(root) {
			local = append(local, pkg)
		}
		return true
	}, nil)
	result := make([]*packages.Package, 0, len(local))
	for _, pkg := range local {
		if strings.HasSuffix(pkg.ID, ".test") {
			continue
		}
		if pkg.Types == nil || pkg.TypesInfo == nil || (len(pkg.Syntax) == 0 && !hasTestVariant(pkg, local)) {
			return nil, fmt.Errorf("%w: %s", ErrTypeAnalysis, pkg.ID)
		}
		result = append(result, pkg)
	}
	if len(result) == 0 {
		return nil, ErrEmptySources
	}
	return result, nil
}

func hasTestVariant(pkg *packages.Package, graph []*packages.Package) bool {
	for _, candidate := range graph {
		matching := candidate.PkgPath == pkg.PkgPath || candidate.PkgPath == pkg.PkgPath+"_test"
		if matching && candidate.ID != pkg.ID && len(candidate.Syntax) > 0 {
			return true
		}
	}
	return false
}

func inspectGoPackage(root, build string, pkg *packages.Package) (GoPackageInventory, error) {
	item := GoPackageInventory{
		ID: pkg.ID, ImportPath: pkg.PkgPath, Build: build,
		Files: make([]string, 0, len(pkg.CompiledGoFiles)), Imports: make([]string, 0, len(pkg.Imports)),
		Symbols: inventorySymbols(root, pkg), References: inventoryReferences(root, pkg),
	}
	for _, name := range pkg.CompiledGoFiles {
		relative, err := filepath.Rel(root, name)
		if err != nil || !safeRepositoryPath(filepath.ToSlash(relative)) {
			return GoPackageInventory{}, fmt.Errorf("%w: compiled source outside repository", ErrTypeAnalysis)
		}
		item.Files = append(item.Files, filepath.ToSlash(relative))
	}
	for dependency := range pkg.Imports {
		item.Imports = append(item.Imports, dependency)
	}
	slices.Sort(item.Files)
	slices.Sort(item.Imports)
	return item, nil
}
