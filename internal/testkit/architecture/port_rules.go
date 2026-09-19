package architecture

import (
	"go/types"
	"path"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// PortInventory connects an actual interface method to its concrete repository implementations.
type PortInventory struct {
	Symbol            string         `json:"symbol"`
	File              string         `json:"file"`
	Line              int            `json:"line"`
	Module            string         `json:"module"`
	Owner             string         `json:"owner"`
	Signature         string         `json:"signature"`
	Repositories      []string       `json:"repositories"`
	RepositoryMethods []string       `json:"repositoryMethods"`
	Consumers         []PortConsumer `json:"consumers"`
}

type ownedType struct {
	value *types.Named
}

func inspectPortGraph(
	root string, graph []*packages.Package, registry OwnershipRegistry,
) ([]PortInventory, []Violation) {
	owners := make(map[string]PackageOwnership)
	for _, owner := range registry.Packages {
		owners[owner.Path] = owner
	}
	repositories := collectRepoTypes(root, graph, owners)
	ports := make([]PortInventory, 0)
	violations := make([]Violation, 0)
	for _, pkg := range graph {
		if pkg.ID != pkg.PkgPath {
			continue
		}
		nextPorts, nextViolations := inspectModelPorts(root, pkg, owners, repositories)
		ports = append(ports, nextPorts...)
		violations = append(violations, nextViolations...)
	}
	return ports, violations
}

func collectRepoTypes(
	root string, graph []*packages.Package, owners map[string]PackageOwnership,
) []ownedType {
	result := make([]ownedType, 0)
	for _, pkg := range graph {
		if pkg.ID != pkg.PkgPath {
			continue
		}
		for _, name := range pkg.Types.Scope().Names() {
			object, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
			if !ok || object.IsAlias() {
				continue
			}
			position := inventoryPosition(root, pkg.Fset, object.Pos())
			if owners[path.Dir(position.Filename)].Layer != "repo" {
				continue
			}
			value, ok := object.Type().(*types.Named)
			if ok {
				result = append(result, ownedType{value: value})
			}
		}
	}
	return result
}

func inspectModelPorts(
	root string, pkg *packages.Package, owners map[string]PackageOwnership, repositories []ownedType,
) ([]PortInventory, []Violation) {
	ports := make([]PortInventory, 0)
	violations := make([]Violation, 0)
	for _, name := range pkg.Types.Scope().Names() {
		object, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
		if !ok || object.IsAlias() {
			continue
		}
		position := inventoryPosition(root, pkg.Fset, object.Pos())
		owner := owners[path.Dir(position.Filename)]
		if owner.Layer != "model" || strings.HasSuffix(position.Filename, "_test.go") {
			continue
		}
		port, ok := object.Type().Underlying().(*types.Interface)
		if !ok {
			continue
		}
		implementations := repositoryImplementations(port.Complete(), repositories)
		for index := range port.NumMethods() {
			method := port.Method(index)
			location := inventoryPosition(root, pkg.Fset, method.Pos())
			record := PortInventory{
				Symbol: inventoryObjectID(method), File: location.Filename, Line: location.Line,
				Owner: owner.Owner, Module: owner.Module, Signature: types.TypeString(method.Type(), packagePath),
				Repositories:      implementations,
				RepositoryMethods: repositoryMethodDefinitions(port, method, repositories),
				Consumers:         []PortConsumer{},
			}
			ports = append(ports, record)
			violations = append(violations, inspectPortMethod(record, method, len(implementations) > 0)...)
		}
	}
	return ports, violations
}

func repositoryImplementations(port *types.Interface, repositories []ownedType) []string {
	implementations := make([]string, 0)
	if port.NumMethods() == 0 {
		return implementations
	}
	for _, repository := range repositories {
		pointer := types.NewPointer(repository.value)
		if types.Implements(pointer, port) || types.Implements(repository.value, port) {
			implementations = append(implementations, types.TypeString(pointer, packagePath))
		}
	}
	slices.Sort(implementations)
	return slices.Compact(implementations)
}

func inspectPortMethod(record PortInventory, method *types.Func, repository bool) []Violation {
	signature, ok := method.Type().(*types.Signature)
	if !ok {
		return []Violation{portViolation(record, TypeIssue{Kind: "unresolved method", Path: []string{record.Symbol}}, "AR03")}
	}
	violations := inspectPortTuple(record, signature.Params(), true, repository)
	return append(violations, inspectPortTuple(record, signature.Results(), false, repository)...)
}

func inspectPortTuple(record PortInventory, tuple *types.Tuple, input, repository bool) []Violation {
	violations := make([]Violation, 0)
	for index := range tuple.Len() {
		value := tuple.At(index).Type()
		if allowedPortPosition(value, index, input, repository) {
			continue
		}
		for _, issue := range InspectValueType(value) {
			violations = append(violations, portViolation(record, issue, "AR03"))
			if repository && slices.Contains([]string{"executable callback", "capability interface"}, issue.Kind) {
				violations = append(violations, portViolation(record, issue, "AR04"))
			}
		}
	}
	return violations
}

func portViolation(record PortInventory, issue TypeIssue, rule string) Violation {
	return Violation{
		Rule: rule, File: record.File, Line: record.Line, Symbol: record.Symbol,
		DependencyChain: append([]string{record.Symbol}, issue.Path...), Message: issue.Kind + " crosses a business port",
	}
}
