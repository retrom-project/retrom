package architecture

import "go/types"

func (analysis *archiveResourceAnalysis) proveResult(
	method *types.Func, results *types.Tuple, index int,
) ArchiveResourceProof {
	if index != 0 || results.Len() != 2 ||
		!types.Identical(results.At(1).Type(), types.Universe.Lookup("error").Type()) {
		return ArchiveResourceProof{
			Resource: types.TypeString(results.At(index).Type(), packagePath),
			Status:   "WRONG_SHAPE", Reason: "archive acquisition must return exactly (resource, error)",
			Bindings: []ArchiveResourceBinding{}, Sources: []SourceFile{},
		}
	}
	if reason := archiveAcquisitionInputs(method); reason != "" {
		return ArchiveResourceProof{
			Resource: types.TypeString(results.At(index).Type(), packagePath),
			Status:   "WRONG_SHAPE", Reason: reason,
			Bindings: []ArchiveResourceBinding{}, Sources: []SourceFile{},
		}
	}
	return analysis.prove(method, results.At(index).Type())
}

func archiveAcquisitionInputs(method *types.Func) string {
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Variadic() || signature.TypeParams().Len() != 0 ||
		signature.RecvTypeParams().Len() != 0 || signature.Params().Len() == 0 {
		return "archive acquisition requires a fixed context and closed-value parameter list"
	}
	first, ok := types.Unalias(signature.Params().At(0).Type()).(*types.Named)
	if !ok || first.Obj().Pkg() == nil || first.Obj().Pkg().Path() != "context" || first.Obj().Name() != "Context" {
		return "archive acquisition requires context.Context as its first parameter"
	}
	for index := 1; index < signature.Params().Len(); index++ {
		if len(InspectValueType(signature.Params().At(index).Type())) != 0 {
			return "archive acquisition inputs after context must be recursively closed values"
		}
	}
	return ""
}

func labelArchiveBuild(ports []PortInventory, build string) {
	for index := range ports {
		for resource := range ports[index].ArchiveResources {
			ports[index].ArchiveResources[resource].Build = build
		}
	}
}
