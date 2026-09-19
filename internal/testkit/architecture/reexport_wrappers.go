package architecture

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

func wrapperTargets(info *types.Info, declaration *ast.FuncDecl) []types.Object {
	expression := singleWrapperExpression(declaration)
	if expression == nil {
		return nil
	}
	function, ok := info.Defs[declaration.Name].(*types.Func)
	if !ok {
		return nil
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok {
		return nil
	}
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		if signature.Params().Len() != 0 {
			return nil
		}
		return directForwardedObject(info, expression)
	}
	if len(call.Args) != signature.Params().Len() || call.Ellipsis.IsValid() != signature.Variadic() {
		return nil
	}
	for index, argument := range call.Args {
		if callObject(info, argument) != signature.Params().At(index) {
			return nil
		}
	}
	object, ok := callObject(info, call.Fun).(*types.Func)
	if !ok {
		return nil
	}
	targetSignature, ok := object.Type().(*types.Signature)
	if !ok || targetSignature.Recv() != nil {
		return nil
	}
	return []types.Object{object}
}

func singleWrapperExpression(declaration *ast.FuncDecl) ast.Expr {
	if declaration.Recv != nil || declaration.Body == nil || len(declaration.Body.List) != 1 {
		return nil
	}
	switch statement := declaration.Body.List[0].(type) {
	case *ast.ReturnStmt:
		if len(statement.Results) == 1 {
			return statement.Results[0]
		}
	case *ast.ExprStmt:
		return statement.X
	}
	return nil
}

func attachReexportConsumers(records []ReexportInventory, graph []GoPackageInventory) {
	bySymbol := make(map[string][]GoReference, len(records))
	for _, record := range records {
		bySymbol[record.Symbol] = nil
	}
	for _, pkg := range graph {
		for _, reference := range pkg.References {
			if _, exists := bySymbol[reference.Target]; exists {
				bySymbol[reference.Target] = append(bySymbol[reference.Target], reference)
			}
		}
	}
	for index := range records {
		references := bySymbol[records[index].Symbol]
		slices.SortFunc(references, compareGoReferences)
		records[index].Consumers = slices.CompactFunc(references, func(left, right GoReference) bool {
			return compareGoReferences(left, right) == 0
		})
	}
}

func compareReexports(left, right ReexportInventory) int {
	return strings.Compare(left.Symbol+"\x00"+left.File, right.Symbol+"\x00"+right.File)
}
