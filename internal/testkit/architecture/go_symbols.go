package architecture

import (
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

func inventorySymbols(root string, pkg *packages.Package) []GoSymbol {
	result := make([]GoSymbol, 0)
	for name, object := range pkg.TypesInfo.Defs {
		if !inventoryObject(object) {
			continue
		}
		position := inventoryPosition(root, pkg.Fset, name.Pos())
		result = append(result, GoSymbol{
			Name: inventoryObjectID(object), Kind: inventoryObjectKind(object),
			Type: types.TypeString(object.Type(), packageQualifier),
			File: position.Filename, Line: position.Line, Exported: object.Exported(),
		})
	}
	slices.SortFunc(result, func(left, right GoSymbol) int {
		return strings.Compare(left.Name+"\x00"+left.File, right.Name+"\x00"+right.File)
	})
	return result
}

func inventoryReferences(root string, pkg *packages.Package) []GoReference {
	result := make([]GoReference, 0)
	for name, object := range pkg.TypesInfo.Uses {
		if !inventoryObject(object) || object.Pkg() == nil || !strings.HasPrefix(object.Pkg().Path(), "retrom/") {
			continue
		}
		position := inventoryPosition(root, pkg.Fset, name.Pos())
		definition := inventoryPosition(root, pkg.Fset, object.Pos())
		result = append(result, GoReference{
			DefinitionFile: definition.Filename, DefinitionLine: definition.Line,
			Target: inventoryObjectID(object), File: position.Filename, Line: position.Line, Column: position.Column,
		})
	}
	slices.SortFunc(result, compareGoReferences)
	return result
}

func inventoryObject(object types.Object) bool {
	if object == nil || object.Pkg() == nil {
		return false
	}
	switch value := object.(type) {
	case *types.Func, *types.TypeName:
		return true
	case *types.Var:
		return value.IsField() || value.Parent() == value.Pkg().Scope()
	case *types.Const:
		return value.Parent() == value.Pkg().Scope()
	default:
		return false
	}
}

func inventoryObjectKind(object types.Object) string {
	switch value := object.(type) {
	case *types.Func:
		return "function"
	case *types.TypeName:
		if value.IsAlias() {
			return "alias"
		}
		if _, ok := value.Type().Underlying().(*types.Interface); ok {
			return "interface"
		}
		return "type"
	case *types.Var:
		return "variable"
	case *types.Const:
		return "constant"
	default:
		return "unknown"
	}
}

func inventoryObjectID(object types.Object) string {
	function, ok := object.(*types.Func)
	if ok {
		return function.Origin().FullName()
	}
	return object.Pkg().Path() + "." + object.Name()
}

func inventoryPosition(root string, files *token.FileSet, position token.Pos) token.Position {
	result := files.Position(position)
	relative, err := filepath.Rel(root, result.Filename)
	if err == nil && safeRepositoryPath(filepath.ToSlash(relative)) {
		result.Filename = filepath.ToSlash(relative)
	} else {
		result.Filename = "<external>"
	}
	return result
}

func packageQualifier(pkg *types.Package) string {
	return pkg.Path()
}

func compareGoReferences(left, right GoReference) int {
	if comparison := strings.Compare(left.File, right.File); comparison != 0 {
		return comparison
	}
	if left.Line != right.Line {
		return left.Line - right.Line
	}
	if left.Column != right.Column {
		return left.Column - right.Column
	}
	return strings.Compare(left.Target, right.Target)
}
