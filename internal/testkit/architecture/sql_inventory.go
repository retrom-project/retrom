package architecture

import (
	"go/ast"
	"go/constant"
	"go/types"
	"slices"
)

// SQLInventory preserves compile-time SQL text; dynamic queries remain explicitly unresolved.
type SQLInventory struct {
	Constant bool   `json:"constant"`
	Text     string `json:"text,omitempty"`
}

func inspectSQLArgument(info *types.Info, call *ast.CallExpr, function *types.Func) *SQLInventory {
	if !sqlExecutionMethod(function) {
		return nil
	}
	if !slices.Contains([]string{
		"Exec", "ExecContext", "Query", "QueryContext", "QueryRow", "QueryRowContext", "Prepare", "PrepareContext",
	}, function.Name()) {
		return nil
	}
	signature, ok := info.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return nil
	}
	for index := range signature.Params().Len() {
		value, ok := signature.Params().At(index).Type().Underlying().(*types.Basic)
		if !ok || value.Kind() != types.String || index >= len(call.Args) {
			continue
		}
		argument := info.Types[call.Args[index]].Value
		if argument == nil || argument.Kind() != constant.String {
			return &SQLInventory{}
		}
		return &SQLInventory{Constant: true, Text: constant.StringVal(argument)}
	}
	return nil
}
