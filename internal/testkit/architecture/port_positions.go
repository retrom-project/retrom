package architecture

import (
	"go/types"
)

func allowedPortPosition(value types.Type, index int, input, repository bool) bool {
	if types.Identical(value, types.Universe.Lookup("error").Type()) {
		return !input
	}
	named, ok := types.Unalias(value).(*types.Named)
	if !ok {
		return false
	}
	object := named.Obj()
	if object.Pkg() != nil && object.Pkg().Path() == "context" && object.Name() == "Context" {
		return input && index == 0
	}
	return !repository && (streamPort(value) || !input && closeOnlyResource(value))
}

// A lease may cross a non-Repo port only as a direct result. Its sole
// operation releases the acquired resource; it cannot carry business methods.
// The value walker still rejects the same handle inside a Command or Snapshot.
func closeOnlyResource(value types.Type) bool {
	resource, ok := value.Underlying().(*types.Interface)
	if !ok || resource.Complete().NumMethods() != 1 {
		return false
	}
	method := resource.Method(0)
	signature, ok := method.Type().(*types.Signature)
	return ok && method.Name() == "Close" &&
		types.TypeString(unnamedSignature(signature), packagePath) == "func() error"
}

func streamPort(value types.Type) bool {
	stream, ok := value.Underlying().(*types.Interface)
	if !ok || stream.Complete().NumMethods() == 0 {
		return false
	}
	// Closed stream methods preserve resource ownership without admitting a
	// disguised arbitrary business callback. Nested stream fields remain invalid.
	allowed := map[string]string{
		"Read":   "func([]byte) (int, error)",
		"ReadAt": "func([]byte, int64) (int, error)",
		"Seek":   "func(int64, int) (int64, error)",
		"Close":  "func() error",
	}
	hasRead := false
	for index := range stream.NumMethods() {
		method := stream.Method(index)
		signature, ok := method.Type().(*types.Signature)
		if !ok {
			return false
		}
		// Parameter names are deliberately discarded; type identity is semantic.
		canonical := unnamedSignature(signature)
		if allowed[method.Name()] != types.TypeString(canonical, packagePath) {
			return false
		}
		hasRead = hasRead || method.Name() == "Read" || method.Name() == "ReadAt"
	}
	return hasRead
}

func unnamedSignature(signature *types.Signature) *types.Signature {
	copyTuple := func(tuple *types.Tuple) *types.Tuple {
		values := make([]*types.Var, 0, tuple.Len())
		for index := range tuple.Len() {
			values = append(values, types.NewVar(0, nil, "", tuple.At(index).Type()))
		}
		return types.NewTuple(values...)
	}
	return types.NewSignatureType(nil, nil, nil, copyTuple(signature.Params()), copyTuple(signature.Results()), false)
}
