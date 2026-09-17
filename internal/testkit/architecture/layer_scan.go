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
		{"capability/", "capability"},
		{"capability", "capability"},
		{"foundation/", "foundation"},
		{"foundation", "foundation"},
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
	if err := validateRoot(root); err != nil {
		return nil, err
	}
	result := &ScanResult{}

	if err := scanImportViolations(root, result); err != nil {
		return result, err
	}
	checkServiceImportsRepo(root, result)
	checkPortCallbackTypes(root, result)
	checkCommandFuncFields(root, result)
	checkServiceReexportsAll(root, result)
	checkModelPurityAll(root, result)
	checkNonRepoDBCapability(root, result)
	buildInventory(root, result)

	sortViolations(result.Violations)
	return result, nil
}

// HasAnalysisErrors returns true if scan encountered analysis errors.
func HasAnalysisErrors(result *ScanResult) bool {
	return len(result.Errors) > 0
}

func validateRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("root directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("root %s: %w", root, os.ErrInvalid)
	}
	goMod := filepath.Join(root, "go.mod")
	if _, err := os.Stat(goMod); err != nil {
		return fmt.Errorf("root missing go.mod: %w", err)
	}
	internalDir := filepath.Join(root, "internal")
	if _, err := os.Stat(internalDir); err != nil {
		return fmt.Errorf("root missing internal directory: %w", err)
	}
	return nil
}

func scanImportViolations(root string, result *ScanResult) error {
	fset := token.NewFileSet()
	skipDirs := map[string]bool{
		"vendor": true, ".git": true, "node_modules": true,
		"testdata": true, "web": true, "data": true,
	}
	err := filepath.WalkDir(filepath.Join(root, "internal"),
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				result.Errors = append(result.Errors,
					fmt.Sprintf("walk error %s: %v", path, walkErr))
				return nil
			}
			if entry.IsDir() {
				if skipDirs[filepath.Base(path)] {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") ||
				strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if parseErr != nil {
				result.Errors = append(result.Errors,
					fmt.Sprintf("parse %s: %v", rel, parseErr))
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
	fset *token.FileSet, file *ast.File,
	rel, srcLayer string,
	forbidden map[string]bool,
	result *ScanResult,
) {
	for _, imp := range file.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value)
		depLayer := LayerFromImport(importPath)
		if forbidden[depLayer] {
			pos := fset.Position(imp.Pos())
			result.Violations = append(result.Violations, Violation{
				Rule:   RuleModelRepoNoServiceDep,
				File:   rel,
				Line:   pos.Line,
				Symbol: importPath,
				Message: fmt.Sprintf(
					"%s layer (%s) imports forbidden %s layer (%s)",
					srcLayer, rel, depLayer, importPath,
				),
			})
		}
	}
}

// LAYER-002: service must not import concrete repo or SQL packages.
func checkServiceImportsRepo(root string, result *ScanResult) {
	serviceDir := filepath.Join(root, "internal/service")
	fset := token.NewFileSet()
	walkGoFiles(serviceDir, func(path, rel string) {
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("parse %s: %v", rel, err))
			return
		}
		relFromRoot, _ := filepath.Rel(root, path)
		for _, imp := range file.Imports {
			importPath, _ := strconv.Unquote(imp.Path.Value)
			if isRepoOrSQL(importPath) {
				pos := fset.Position(imp.Pos())
				result.Violations = append(result.Violations, Violation{
					Rule:    RuleServiceNoRepoDep,
					File:    relFromRoot,
					Line:    pos.Line,
					Symbol:  importPath,
					Message: fmt.Sprintf("service imports repo/SQL package %s", importPath),
				})
			}
		}
	})
}

func isRepoOrSQL(importPath string) bool {
	if importPath == "database/sql" || strings.HasPrefix(importPath, "database/sql/") {
		return true
	}
	if strings.HasPrefix(importPath, moduleRoot+"repo/") || importPath == moduleRoot+"repo" {
		return true
	}
	return false
}

// LAYER-003: Repository port params/returns must not contain func types.
func checkPortCallbackTypes(root string, result *ScanResult) {
	modelDir := filepath.Join(root, "internal/model")
	fset := token.NewFileSet()
	walkGoFiles(modelDir, func(path, _ string) {
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			return
		}
		relFromRoot, _ := filepath.Rel(root, path)
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				checkInterfaceForFuncParams(fset, ts, relFromRoot, result)
				checkStructForFuncFields(fset, ts, relFromRoot, result)
			}
		}
	})
}

func checkInterfaceForFuncParams(
	fset *token.FileSet, ts *ast.TypeSpec, rel string, result *ScanResult,
) {
	iface, ok := ts.Type.(*ast.InterfaceType)
	if !ok || iface.Methods == nil {
		return
	}
	isRepoPort := isRepositoryPortName(ts.Name.Name)
	if !isRepoPort {
		return
	}
	for _, method := range iface.Methods.List {
		ft, ok := method.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		methodName := ""
		if len(method.Names) > 0 {
			methodName = method.Names[0].Name
		}
		checkFuncTypeForCallbacks(fset, ft, ts.Name.Name, methodName, rel, result)
	}
}

func checkStructForFuncFields(
	fset *token.FileSet, ts *ast.TypeSpec, rel string, result *ScanResult,
) {
	st, ok := ts.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return
	}
	if !isCommandOrPlanName(ts.Name.Name) {
		return
	}
	for _, field := range st.Fields.List {
		if containsFuncType(field.Type) {
			pos := fset.Position(field.Pos())
			fieldName := ""
			if len(field.Names) > 0 {
				fieldName = field.Names[0].Name
			}
			result.Violations = append(result.Violations, Violation{
				Rule:   RuleNoHiddenCapability,
				File:   rel,
				Line:   pos.Line,
				Symbol: fmt.Sprintf("%s.%s", ts.Name.Name, fieldName),
				Message: fmt.Sprintf(
					"command/plan %s contains func field %s",
					ts.Name.Name, fieldName,
				),
			})
		}
	}
}

func checkFuncTypeForCallbacks(
	fset *token.FileSet, ft *ast.FuncType,
	typeName, methodName, rel string,
	result *ScanResult,
) {
	params := collectParams(ft)
	for _, p := range params {
		if containsFuncType(p.typ) {
			pos := fset.Position(p.pos)
			result.Violations = append(result.Violations, Violation{
				Rule: RulePortNoCallback,
				File: rel,
				Line: pos.Line,
				Symbol: fmt.Sprintf(
					"%s.%s (param %s)", typeName, methodName, p.name,
				),
				Message: fmt.Sprintf(
					"repository port %s.%s accepts func parameter",
					typeName, methodName,
				),
			})
		}
	}
}

type paramInfo struct {
	name string
	typ  ast.Expr
	pos  token.Pos
}

func collectParams(ft *ast.FuncType) []paramInfo {
	var params []paramInfo
	if ft.Params != nil {
		for _, f := range ft.Params.List {
			name := "_"
			if len(f.Names) > 0 {
				name = f.Names[0].Name
			}
			params = append(params, paramInfo{name: name, typ: f.Type, pos: f.Pos()})
		}
	}
	return params
}

func containsFuncType(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch t := expr.(type) {
	case *ast.FuncType:
		return true
	case *ast.Ident:
		return false
	case *ast.SelectorExpr:
		return false
	case *ast.StarExpr:
		return containsFuncType(t.X)
	case *ast.ArrayType:
		return containsFuncType(t.Elt)
	case *ast.MapType:
		return containsFuncType(t.Key) || containsFuncType(t.Value)
	case *ast.InterfaceType:
		return false
	case *ast.ChanType:
		return true
	case *ast.Ellipsis:
		return containsFuncType(t.Elt)
	}
	return false
}

func isRepositoryPortName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "repository") ||
		strings.Contains(lower, "writer") ||
		strings.Contains(lower, "reader") ||
		strings.Contains(lower, "records") ||
		strings.Contains(lower, "scope") ||
		strings.HasSuffix(lower, "port")
}

func isCommandOrPlanName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "command") ||
		strings.Contains(lower, "plan") ||
		strings.Contains(lower, "mutation")
}

// LAYER-004: check commands/plans for func fields (done in checkStructForFuncFields above).

// checkCommandFuncFields scans all model structs for hidden capabilities.
func checkCommandFuncFields(root string, result *ScanResult) {
	modelDir := filepath.Join(root, "internal/model")
	fset := token.NewFileSet()
	walkGoFiles(modelDir, func(path, _ string) {
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			return
		}
		relFromRoot, _ := filepath.Rel(root, path)
		scanStructFuncFields(fset, file, relFromRoot, result)
	})
}

func scanStructFuncFields(
	fset *token.FileSet, file *ast.File, rel string, result *ScanResult,
) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			checkTypeSpecFuncField(fset, spec, rel, result)
		}
	}
}

func checkTypeSpecFuncField(
	fset *token.FileSet, spec ast.Spec, rel string, result *ScanResult,
) {
	ts, ok := spec.(*ast.TypeSpec)
	if !ok || !ts.Name.IsExported() || isCommandOrPlanName(ts.Name.Name) {
		return
	}
	st, ok := ts.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return
	}
	for _, field := range st.Fields.List {
		if !isExportedFuncField(field) {
			continue
		}
		pos := fset.Position(field.Pos())
		result.Violations = append(result.Violations, Violation{
			Rule:    RuleNoHiddenCapability,
			File:    rel,
			Line:    pos.Line,
			Symbol:  fmt.Sprintf("%s.%s", ts.Name.Name, field.Names[0].Name),
			Message: fmt.Sprintf("model struct %s has func field %s", ts.Name.Name, field.Names[0].Name),
		})
	}
}

func isExportedFuncField(field *ast.Field) bool {
	if len(field.Names) == 0 {
		return false
	}
	return field.Names[0].IsExported() && isFuncExpr(field.Type)
}

func isFuncExpr(expr ast.Expr) bool {
	_, ok := expr.(*ast.FuncType)
	return ok
}

// checkServiceReexportsAll detects LAYER-006 violations:
// service re-exporting model types via alias, var, const, or wrapper.
func checkServiceReexportsAll(root string, result *ScanResult) {
	serviceDir := filepath.Join(root, "internal/service")
	entries, err := os.ReadDir(serviceDir)
	if err != nil {
		result.Errors = append(result.Errors,
			fmt.Sprintf("read service dir: %v", err))
		return
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		domain := entry.Name()
		pkgDir := filepath.Join(serviceDir, domain)
		walkGoFiles(pkgDir, func(path, _ string) {
			file, parseErr := parser.ParseFile(fset, path, nil, parser.AllErrors)
			if parseErr != nil {
				return
			}
			relFromRoot, _ := filepath.Rel(root, path)
			modelImports := collectModelImports(file)
			if len(modelImports) == 0 {
				return
			}
			scanReexportDecls(fset, file, relFromRoot, modelImports, result)
		})
	}
}

func collectModelImports(file *ast.File) map[string]string {
	imports := make(map[string]string)
	for _, imp := range file.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value)
		if !strings.HasPrefix(importPath, moduleRoot+"model") {
			continue
		}
		localName := ""
		if imp.Name != nil {
			localName = imp.Name.Name
		} else {
			parts := strings.Split(importPath, "/")
			localName = parts[len(parts)-1]
		}
		imports[localName] = importPath
	}
	return imports
}

func scanReexportDecls(
	fset *token.FileSet, file *ast.File,
	rel string, modelImports map[string]string,
	result *ScanResult,
) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		if gd.Tok == token.TYPE {
			scanTypeReexports(fset, gd, rel, modelImports, result)
		}
		if gd.Tok == token.VAR || gd.Tok == token.CONST {
			scanValueReexports(fset, gd, rel, modelImports, result)
		}
	}
}

func scanTypeReexports(
	fset *token.FileSet, gd *ast.GenDecl,
	rel string, modelImports map[string]string, result *ScanResult,
) {
	for _, spec := range gd.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || !ts.Name.IsExported() {
			continue
		}
		if ts.Assign != 0 && isModelSelector(ts.Type, modelImports) {
			pos := fset.Position(ts.Pos())
			result.Violations = append(result.Violations, Violation{
				Rule:    RuleServiceNoModelReexport,
				File:    rel,
				Line:    pos.Line,
				Symbol:  ts.Name.Name,
				Message: fmt.Sprintf("service re-exports model type %s", ts.Name.Name),
			})
		}
	}
}

func scanValueReexports(
	fset *token.FileSet, gd *ast.GenDecl,
	rel string, modelImports map[string]string, result *ScanResult,
) {
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range vs.Names {
			if !name.IsExported() {
				continue
			}
			if i < len(vs.Values) && isModelSelector(vs.Values[i], modelImports) {
				pos := fset.Position(name.Pos())
				result.Violations = append(result.Violations, Violation{
					Rule:    RuleServiceNoModelReexport,
					File:    rel,
					Line:    pos.Line,
					Symbol:  name.Name,
					Message: fmt.Sprintf("service re-exports model %s %s", gd.Tok, name.Name),
				})
			}
		}
	}
}

func isModelSelector(expr ast.Expr, modelImports map[string]string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, isModel := modelImports[ident.Name]
	return isModel
}

// checkModelPurityAll detects LAYER-007 violations including forbidden
// imports, time.Now/UUID/random calls, and forbidden function calls.
func checkModelPurityAll(root string, result *ScanResult) {
	modelDir := filepath.Join(root, "internal/model")
	fset := token.NewFileSet()

	walkGoFiles(modelDir, func(path, _ string) {
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			relFromRoot, _ := filepath.Rel(root, path)
			result.Errors = append(result.Errors,
				fmt.Sprintf("parse model %s: %v", relFromRoot, err))
			return
		}
		relFromRoot, _ := filepath.Rel(root, path)
		checkModelForbiddenImports(fset, file, relFromRoot, result)
		checkModelImpureCalls(fset, file, relFromRoot, result)
	})
}

var forbiddenModelImports = map[string]bool{
	"os": true, "os/exec": true, "os/signal": true,
	"net": true, "net/http": true,
	"database/sql": true, "syscall": true,
	"math/rand": true, "math/rand/v2": true,
	"crypto/rand": true,
}

var impureCallPatterns = map[string][]string{
	"time":                   {"Now"},
	"uuid":                   {"New", "NewV7", "NewRandom"},
	"github.com/google/uuid": {"New", "NewV7", "NewRandom"},
}

func checkModelForbiddenImports(
	fset *token.FileSet, file *ast.File, rel string, result *ScanResult,
) {
	for _, imp := range file.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value)
		if forbiddenModelImports[importPath] {
			pos := fset.Position(imp.Pos())
			result.Violations = append(result.Violations, Violation{
				Rule:    RuleModelNoPureViolation,
				File:    rel,
				Line:    pos.Line,
				Symbol:  importPath,
				Message: fmt.Sprintf("model imports forbidden I/O package %s", importPath),
			})
		}
		if strings.HasPrefix(importPath, moduleRoot+"repo") {
			pos := fset.Position(imp.Pos())
			result.Violations = append(result.Violations, Violation{
				Rule:    RuleModelRepoNoServiceDep,
				File:    rel,
				Line:    pos.Line,
				Symbol:  importPath,
				Message: fmt.Sprintf("model imports repo package %s", importPath),
			})
		}
	}
}

func checkModelImpureCalls(
	fset *token.FileSet, file *ast.File, rel string, result *ScanResult,
) {
	localNames := buildImportLocalNames(file)
	ast.Inspect(file, func(n ast.Node) bool {
		v := matchImpureCall(n, localNames)
		if v == nil {
			return true
		}
		v.File = rel
		pos := fset.Position(n.Pos())
		v.Line = pos.Line
		result.Violations = append(result.Violations, *v)
		return true
	})
}

func matchImpureCall(n ast.Node, localNames map[string]string) *Violation {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil
	}
	importPath, found := localNames[ident.Name]
	if !found {
		return nil
	}
	patterns := impureCallPatterns[importPath]
	if len(patterns) == 0 {
		parts := strings.Split(importPath, "/")
		patterns = impureCallPatterns[parts[len(parts)-1]]
	}
	for _, pat := range patterns {
		if sel.Sel.Name == pat {
			return &Violation{
				Rule:   RuleModelNoPureViolation,
				Symbol: fmt.Sprintf("%s.%s", ident.Name, sel.Sel.Name),
				Message: fmt.Sprintf(
					"model calls impure function %s.%s",
					ident.Name, sel.Sel.Name,
				),
			}
		}
	}
	return nil
}

func buildImportLocalNames(file *ast.File) map[string]string {
	names := make(map[string]string)
	for _, imp := range file.Imports {
		importPath, _ := strconv.Unquote(imp.Path.Value)
		if imp.Name != nil {
			names[imp.Name.Name] = importPath
		} else {
			parts := strings.Split(importPath, "/")
			names[parts[len(parts)-1]] = importPath
		}
	}
	return names
}

// LAYER-008: Non-repo code must not obtain dbexec/sql.Tx/Rows capability.
func checkNonRepoDBCapability(root string, result *ScanResult) {
	dbPackages := map[string]bool{
		"database/sql":             true,
		moduleRoot + "repo/dbexec": true,
	}
	fset := token.NewFileSet()
	internalDir := filepath.Join(root, "internal")
	walkGoFiles(internalDir, func(path, _ string) {
		relFromRoot, _ := filepath.Rel(root, path)
		layer := layerFromFile(relFromRoot)
		if layer == "repo" || layer == "testkit" || layer == "" {
			return
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return
		}
		for _, imp := range file.Imports {
			importPath, _ := strconv.Unquote(imp.Path.Value)
			if dbPackages[importPath] && layer != "bootstrap" {
				// bootstrap/composition legitimately wires *sql.DB
				if layer == "adapter" || layer == "capability" {
					continue
				}
				pos := fset.Position(imp.Pos())
				result.Violations = append(result.Violations, Violation{
					Rule:    RuleNonRepoNoDBCapability,
					File:    relFromRoot,
					Line:    pos.Line,
					Symbol:  importPath,
					Message: fmt.Sprintf("%s layer imports DB package %s", layer, importPath),
				})
			}
		}
	})
}

// buildInventory creates a basic inventory of model ports and their status.
func buildInventory(root string, result *ScanResult) {
	modelDir := filepath.Join(root, "internal/model")
	fset := token.NewFileSet()
	walkGoFiles(modelDir, func(path, _ string) {
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			return
		}
		relFromRoot, _ := filepath.Rel(root, path)
		pkgName := file.Name.Name
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				item := InventoryItem{
					Package: pkgName,
					Name:    ts.Name.Name,
					Layer:   "model",
				}
				switch ts.Type.(type) {
				case *ast.InterfaceType:
					item.Kind = "interface"
				case *ast.StructType:
					item.Kind = "struct"
				case *ast.FuncType:
					item.Kind = "functype"
				default:
					item.Kind = "type"
				}
				item.Status = classifyInventoryStatus(ts, relFromRoot, result)
				result.Inventory = append(result.Inventory, item)
			}
		}
		// Also scan exported funcs
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || !fd.Name.IsExported() || fd.Recv != nil {
				continue
			}
			result.Inventory = append(result.Inventory, InventoryItem{
				Package: pkgName,
				Name:    fd.Name.Name,
				Kind:    "func",
				Layer:   "model",
				Status:  "pending",
			})
		}
	})
}

func classifyInventoryStatus(ts *ast.TypeSpec, rel string, result *ScanResult) string {
	for _, v := range result.Violations {
		if v.File == rel && strings.Contains(v.Symbol, ts.Name.Name) {
			return "violation"
		}
	}
	iface, ok := ts.Type.(*ast.InterfaceType)
	if !ok {
		return "pending"
	}
	if iface.Methods == nil {
		return "pending"
	}
	for _, method := range iface.Methods.List {
		ft, ok := method.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		if ft.Params != nil {
			for _, p := range ft.Params.List {
				if containsFuncType(p.Type) {
					return "violation"
				}
			}
		}
	}
	return "pending"
}

// walkGoFiles iterates non-test .go files under dir.
func walkGoFiles(dir string, fn func(path, relPath string)) {
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		fn(path, rel)
		return nil
	})
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
