package architecture

import (
	"go/types"
	"path"
	"strings"

	"golang.org/x/tools/go/packages"
)

// ArchiveResourceProof is generated from one compiled build, never a symbol allowlist.
type ArchiveResourceProof struct {
	Build    string                   `json:"build"`
	Resource string                   `json:"resource"`
	Status   string                   `json:"status"`
	Reason   string                   `json:"reason,omitempty"`
	Bindings []ArchiveResourceBinding `json:"bindings"`
	Sources  []SourceFile             `json:"sources"`
}

// ArchiveResourceBinding records the construction, invocation and return origins.
type ArchiveResourceBinding struct {
	Consumer  ResourceLocation   `json:"consumer"`
	Injection ResourceLocation   `json:"injection"`
	Factory   ResourceLocation   `json:"factory"`
	Return    ResourceLocation   `json:"return"`
	Concrete  string             `json:"concrete"`
	Methods   []ResourceLocation `json:"methods"`
}

// ResourceLocation is a resolved production definition or use.
type ResourceLocation struct {
	Symbol string `json:"symbol"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

type archiveContract struct {
	header, content, entry, nested types.Type
	owners                         map[string]PackageOwnership
	root                           string
	graph                          []*packages.Package
}

func newArchiveContract(
	root string, graph []*packages.Package, owners map[string]PackageOwnership,
) archiveContract {
	result := archiveContract{root: root, graph: graph, owners: owners}
	for _, pkg := range graph {
		if pkg.ID != pkg.PkgPath || !strings.HasSuffix(pkg.PkgPath, "/internal/capability/format/importing") {
			continue
		}
		result.header = archiveFact(pkg, "ArchiveMemberHeader")
		result.content = archiveFact(pkg, "ArchiveContent")
		result.entry = archiveFact(pkg, "ArchiveEntry")
		result.nested = archiveFact(pkg, "NestedArchiveFormat")
	}
	return result
}

func archiveFact(pkg *packages.Package, name string) types.Type {
	value := archiveFactFromPackage(pkg.Types, name)
	if value == nil {
		return nil
	}
	return value
}

func archiveFactFromPackage(pkg *types.Package, name string) *types.Named {
	object, ok := pkg.Scope().Lookup(name).(*types.TypeName)
	if !ok || object.IsAlias() {
		return nil
	}
	value, ok := object.Type().(*types.Named)
	if !ok || genericResourceType(value) {
		return nil
	}
	return value
}

func archiveReaderCandidate(value types.Type) bool {
	contract, ok := value.Underlying().(*types.Interface)
	if !ok {
		return false
	}
	names := make(map[string]bool)
	for index := range contract.Complete().NumMethods() {
		names[contract.Method(index).Name()] = true
	}
	return names["Next"] && names["Read"] && names["Complete"] && names["Close"]
}

func (contract archiveContract) validate(value types.Type) string {
	if genericResourceType(value) {
		return "generic archive resource declaration"
	}
	named, ok := types.Unalias(value).(*types.Named)
	if !ok || contract.layer(named.Obj()) != "model" {
		return "archive resource must be a named Model interface"
	}
	if reason := contract.validateFacts(); reason != "" {
		return reason
	}
	resource, ok := named.Underlying().(*types.Interface)
	if !ok || resource.NumEmbeddeds() != 0 || resource.NumExplicitMethods() != 4 ||
		resource.Complete().NumMethods() != 4 {
		return "archive resource requires exactly four explicit methods without embedding"
	}
	errorType := types.Universe.Lookup("error").Type()
	shapes := map[string]archiveMethodShape{
		"Next": {results: []types.Type{contract.header, errorType}},
		"Read": {
			params:  []types.Type{types.NewSlice(types.Typ[types.Byte])},
			results: []types.Type{types.Typ[types.Int], errorType},
		},
		"Complete": {params: []types.Type{contract.content}, results: []types.Type{contract.entry, errorType}},
		"Close":    {results: []types.Type{errorType}},
	}
	for index := range resource.NumMethods() {
		method := resource.Method(index)
		shape, exists := shapes[method.Name()]
		signature, signatureOK := method.Type().(*types.Signature)
		if !exists || !signatureOK || !shape.matches(signature) {
			return "archive resource method has a noncanonical signature: " + method.Name()
		}
	}
	return ""
}

func genericResourceType(value types.Type) bool {
	switch typed := value.(type) {
	case *types.Alias:
		return typed.TypeParams().Len() != 0 || typed.TypeArgs().Len() != 0 || genericResourceType(typed.Rhs())
	case *types.Named:
		return typed.TypeParams().Len() != 0 || typed.TypeArgs().Len() != 0
	default:
		return false
	}
}

type archiveMethodShape struct {
	params, results []types.Type
}

func (shape archiveMethodShape) matches(signature *types.Signature) bool {
	return !signature.Variadic() && signature.TypeParams().Len() == 0 &&
		signature.RecvTypeParams().Len() == 0 &&
		archiveTupleMatches(signature.Params(), shape.params) && archiveTupleMatches(signature.Results(), shape.results)
}

func archiveTupleMatches(tuple *types.Tuple, expected []types.Type) bool {
	if tuple.Len() != len(expected) {
		return false
	}
	for index, value := range expected {
		if !types.Identical(tuple.At(index).Type(), value) {
			return false
		}
	}
	return true
}

func (contract archiveContract) validateFacts() string {
	for _, value := range []types.Type{contract.header, contract.content, contract.entry, contract.nested} {
		if value == nil || genericResourceType(value) || len(InspectValueType(value)) != 0 {
			return "archive facts are missing, generic or not recursively closed values"
		}
		named, ok := value.(*types.Named)
		if !ok || contract.layer(named.Obj()) != "capability" {
			return "archive facts have no canonical Capability declaration"
		}
	}
	stringType, int64Type := types.Typ[types.String], types.Typ[types.Int64]
	content := map[string]types.Type{
		"Size": int64Type, "CRC32": stringType, "MD5": stringType, "SHA1": stringType, "SHA256": stringType,
	}
	entry := map[string]types.Type{
		"Ordinal": types.Typ[types.Int], "OriginalPath": stringType, "NormalizedPath": stringType,
		"ASCIICasefoldPath": stringType, "ArchiveFormat": stringType, "CompressionProfile": stringType,
		"Size": int64Type, "CRC32": stringType, "MD5": stringType, "SHA1": stringType, "SHA256": stringType,
	}
	nested := contract.nested
	if !types.Identical(nested.Underlying(), stringType) {
		return "archive nested format is not the canonical closed string value"
	}
	entry["NestedArchive"] = nested
	header := map[string]types.Type{"Entry": contract.entry, "Unpacked": types.Typ[types.Bool]}
	if !archiveFieldsMatch(contract.content, content) || !archiveFieldsMatch(contract.entry, entry) ||
		!archiveFieldsMatch(contract.header, header) {
		return "archive facts do not match the fixed field schemas"
	}
	return ""
}

func archiveFieldsMatch(value types.Type, expected map[string]types.Type) bool {
	fields, ok := value.Underlying().(*types.Struct)
	if !ok || fields.NumFields() != len(expected) {
		return false
	}
	for index := range fields.NumFields() {
		field := fields.Field(index)
		want, exists := expected[field.Name()]
		if !exists || field.Embedded() || !types.Identical(field.Type(), want) {
			return false
		}
	}
	return true
}

func (contract archiveContract) layer(object types.Object) string {
	if object == nil || object.Pkg() == nil {
		return ""
	}
	for _, pkg := range contract.graph {
		if pkg.ID == pkg.PkgPath && pkg.Types == object.Pkg() {
			location := inventoryPosition(contract.root, pkg.Fset, object.Pos())
			if !strings.HasSuffix(location.Filename, "_test.go") {
				return contract.owners[path.Dir(location.Filename)].Layer
			}
		}
	}
	return ""
}
