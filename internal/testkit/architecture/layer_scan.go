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
	segments := []struct {
		prefix string
		layer  string
	}{
		{"model/", "model"},
		{"model", "model"},
		{"service/", "service"},
		{"service", "service"},
		{"repo/", "repo"},
		{"repo", "repo"},
		{"transport/", "transport"},
		{"transport", "transport"},
		{"bootstrap/", "bootstrap"},
		{"bootstrap", "bootstrap"},
		{"adapter/", "adapter"},
		{"adapter", "adapter"},
		{"testkit/", "testkit"},
		{"testkit", "testkit"},
	}
	for _, s := range segments {
		if strings.HasPrefix(rel, s.prefix) || rel == s.layer {
			return s.layer
		}
	}
	return "internal-other"
}

// forbiddenDeps maps source layer → layers it must not import.
var forbiddenDeps = map[string]map[string]bool{
	"model": {"service": true, "transport": true, "bootstrap": true, "repo": true},
	"repo":  {"service": true, "transport": true, "bootstrap": true},
}

// ScanDirectory performs a full layering scan of the repository.
func ScanDirectory(root string) (*ScanResult, error) {
	result := &ScanResult{}
	if err := scanImportViolations(root, result); err != nil {
		return nil, err
	}
	if reexports, err := checkServiceReexports(root); err == nil {
		result.Violations = append(result.Violations, reexports...)
	}
	if purity, err := checkModelPurity(root); err == nil {
		result.Violations = append(result.Violations, purity...)
	}
	sortViolations(result.Violations)
	return result, nil
}

func scanImportViolations(root string, result *ScanResult) error {
	fset := token.NewFileSet()
	skipDirs := map[string]bool{
		"vendor": true, ".git": true, "node_modules": true,
		"testdata": true, "web": true, "data": true,
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // skip unreadable entries
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
		forbidden := forbiddenDeps[srcLayer]
		if len(forbidden) == 0 {
			return nil
		}
		collectImportViolations(fset, file, rel, srcLayer, forbidden, result)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk internal directory: %w", err)
	}
	return nil
}

func collectImportViolations(
	fset *token.FileSet, file *ast.File, rel, srcLayer string, forbidden map[string]bool, result *ScanResult,
) {
	for _, imp := range file.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value)
		depLayer := LayerFromImport(importPath)
		if forbidden[depLayer] {
			pos := fset.Position(imp.Pos())
			result.Violations = append(result.Violations, Violation{
				Rule:    RuleModelRepoNoServiceDep,
				File:    rel,
				Line:    pos.Line,
				Symbol:  importPath,
				Message: fmt.Sprintf("%s layer (%s) imports forbidden %s layer (%s)", srcLayer, rel, depLayer, importPath),
			})
		}
	}
}

func checkServiceReexports(root string) ([]Violation, error) {
	serviceDir := filepath.Join(root, "internal/service")
	entries, err := os.ReadDir(serviceDir)
	if err != nil {
		return nil, fmt.Errorf("read service directory: %w", err)
	}
	var violations []Violation
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		domainViolations := scanDomainReexports(root, serviceDir, entry.Name())
		violations = append(violations, domainViolations...)
	}
	return violations, nil
}

func scanDomainReexports(root, serviceDir, domain string) []Violation {
	pkgDir := filepath.Join(serviceDir, domain)
	files, _ := filepath.Glob(filepath.Join(pkgDir, "*.go"))
	modelImportPath := moduleRoot + "model/" + domain
	var violations []Violation
	for _, f := range files {
		if strings.HasSuffix(filepath.Base(f), "_test.go") {
			continue
		}
		vv := scanFileReexports(root, f, modelImportPath, domain)
		violations = append(violations, vv...)
	}
	return violations
}

func scanFileReexports(root, filePath, modelImportPath, domain string) []Violation {
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, filePath, nil, 0)
	if parseErr != nil {
		return nil
	}
	if !hasImport(file, modelImportPath) {
		return nil
	}
	rel, _ := filepath.Rel(root, filePath)
	var violations []Violation
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
			if isModelReexport(ts, domain) {
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
	return violations
}

func hasImport(file *ast.File, importPath string) bool {
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path == importPath {
			return true
		}
	}
	return false
}

func isModelReexport(ts *ast.TypeSpec, domain string) bool {
	sel, ok := ts.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "model" || ident.Name == domain
}

func checkModelPurity(root string) ([]Violation, error) {
	var violations []Violation
	modelDir := filepath.Join(root, "internal/model")

	forbiddenImports := map[string]bool{
		"os": true, "os/exec": true, "os/signal": true,
		"net": true, "net/http": true,
		"database/sql": true, "syscall": true,
	}

	err := filepath.WalkDir(modelDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries and recurse directories
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return nil //nolint:nilerr // skip unparseable files
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
	if err != nil {
		return violations, fmt.Errorf("walk model directory: %w", err)
	}
	return violations, nil
}

func layerFromFile(relPath string) string {
	parts := strings.SplitN(relPath, "/", 4)
	if len(parts) < 2 || parts[0] != "internal" {
		return ""
	}
	return LayerFromImport(moduleRoot + parts[1])
}

func sortViolations(violations []Violation) {
	sort.Slice(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
}
