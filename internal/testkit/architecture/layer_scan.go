package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const moduleRoot = "retrom/internal/"

// LayerFromImport classifies an import path into a layer.
func LayerFromImport(path string) string {
	if !strings.HasPrefix(path, moduleRoot) {
		return "external"
	}
	rel := strings.TrimPrefix(path, moduleRoot)
	switch {
	case strings.HasPrefix(rel, "model/") || rel == "model":
		return "model"
	case strings.HasPrefix(rel, "service/") || rel == "service":
		return "service"
	case strings.HasPrefix(rel, "repo/") || rel == "repo":
		return "repo"
	case strings.HasPrefix(rel, "transport/") || rel == "transport":
		return "transport"
	case strings.HasPrefix(rel, "bootstrap/") || rel == "bootstrap":
		return "bootstrap"
	case strings.HasPrefix(rel, "adapter/") || rel == "adapter":
		return "adapter"
	case strings.HasPrefix(rel, "testkit/") || rel == "testkit":
		return "testkit"
	default:
		return "internal-other"
	}
}

// forbiddenDeps maps source layer → layers it must not import.
var forbiddenDeps = map[string]map[string]bool{
	"model": {"service": true, "transport": true, "bootstrap": true, "repo": true},
	"repo":  {"service": true, "transport": true, "bootstrap": true},
}

// ScanDirectory performs a full layering scan of the repository.
func ScanDirectory(root string) (*ScanResult, error) {
	result := &ScanResult{}
	fset := token.NewFileSet()

	skipDirs := map[string]bool{
		"vendor": true, ".git": true, "node_modules": true,
		"testdata": true, "web": true, "data": true,
	}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if skipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if !strings.HasPrefix(rel, "internal/") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("parse %s: %v", rel, parseErr))
			return nil
		}

		srcLayer := layerFromFile(rel)
		if srcLayer == "" {
			return nil
		}

		forbidden := forbiddenDeps[srcLayer]
		if len(forbidden) == 0 {
			return nil
		}

		for _, imp := range file.Imports {
			importPath, _ := strconv.Unquote(imp.Path.Value)
			depLayer := LayerFromImport(importPath)
			if forbidden[depLayer] {
				pos := fset.Position(imp.Pos())
				rule := RuleModelRepoNoServiceDep
				if srcLayer == "repo" {
					rule = RuleModelRepoNoServiceDep
				}
				result.Violations = append(result.Violations, Violation{
					Rule:    rule,
					File:    rel,
					Line:    pos.Line,
					Symbol:  importPath,
					Message: fmt.Sprintf("%s layer (%s) imports forbidden %s layer (%s)", srcLayer, rel, depLayer, importPath),
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	// LAYER-006: Check service re-exports
	reexportViolations, err := checkServiceReexports(root)
	if err == nil {
		result.Violations = append(result.Violations, reexportViolations...)
	}

	// LAYER-007: Check model purity
	purityViolations, err := checkModelPurity(root)
	if err == nil {
		result.Violations = append(result.Violations, purityViolations...)
	}

	sort.Slice(result.Violations, func(i, j int) bool {
		a, b := result.Violations[i], result.Violations[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})

	return result, nil
}

func checkServiceReexports(root string) ([]Violation, error) {
	var violations []Violation
	serviceDir := filepath.Join(root, "internal/service")

	entries, err := os.ReadDir(serviceDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		domain := entry.Name()
		pkgDir := filepath.Join(serviceDir, domain)
		files, _ := filepath.Glob(filepath.Join(pkgDir, "*.go"))

		modelImportPath := moduleRoot + "model/" + domain

		for _, f := range files {
			if strings.HasSuffix(filepath.Base(f), "_test.go") {
				continue
			}
			rel, _ := filepath.Rel(root, f)
			fset := token.NewFileSet()
			file, parseErr := parser.ParseFile(fset, f, nil, 0)
			if parseErr != nil {
				continue
			}

			hasModelImport := false
			for _, imp := range file.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if path == modelImportPath {
					hasModelImport = true
					break
				}
			}
			if !hasModelImport {
				continue
			}

			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || ts.Assign == 0 || !ts.Name.IsExported() {
						continue
					}
					sel, ok := ts.Type.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					ident, ok := sel.X.(*ast.Ident)
					if !ok {
						continue
					}
					if ident.Name == "model" || ident.Name == domain {
						pos := fset.Position(ts.Pos())
						violations = append(violations, Violation{
							Rule:    RuleServiceNoModelReexport,
							File:    rel,
							Line:    pos.Line,
							Symbol:  ts.Name.Name,
							Message: fmt.Sprintf("service re-exports model type %s", ts.Name.Name),
						})
					}
				}
			}
		}
	}
	return violations, nil
}

func checkModelPurity(root string) ([]Violation, error) {
	var violations []Violation
	modelDir := filepath.Join(root, "internal/model")

	forbiddenImports := map[string]bool{
		"os": true, "os/exec": true, "os/signal": true,
		"net": true, "net/http": true,
		"database/sql": true, "syscall": true,
	}

	err := filepath.WalkDir(modelDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return nil
		}

		rel, _ := filepath.Rel(root, path)

		for _, imp := range file.Imports {
			importPath, _ := strconv.Unquote(imp.Path.Value)
			if forbiddenImports[importPath] {
				pos := fset.Position(imp.Pos())
				violations = append(violations, Violation{
					Rule:    RuleModelNoPureViolation,
					File:    rel,
					Line:    pos.Line,
					Symbol:  importPath,
					Message: fmt.Sprintf("model imports I/O package %s", importPath),
				})
			}
			if strings.HasPrefix(importPath, moduleRoot+"repo") {
				pos := fset.Position(imp.Pos())
				violations = append(violations, Violation{
					Rule:    RuleModelRepoNoServiceDep,
					File:    rel,
					Line:    pos.Line,
					Symbol:  importPath,
					Message: fmt.Sprintf("model imports repo package %s", importPath),
				})
			}
		}
		return nil
	})
	return violations, err
}

func layerFromFile(relPath string) string {
	parts := strings.SplitN(relPath, "/", 4)
	if len(parts) < 2 || parts[0] != "internal" {
		return ""
	}
	return LayerFromImport(moduleRoot + parts[1])
}
