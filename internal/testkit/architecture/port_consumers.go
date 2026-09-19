package architecture

import (
	"go/types"
	"slices"
	"strings"
)

// PortConsumer locates an actual typed invocation in production code.
type PortConsumer struct {
	Symbol string `json:"symbol"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Target string `json:"target"`
}

func repositoryMethodDefinitions(
	port *types.Interface, method *types.Func, repositories []ownedType,
) []string {
	result := make([]string, 0)
	for _, repository := range repositories {
		pointer := types.NewPointer(repository.value)
		if !types.Implements(pointer, port) {
			continue
		}
		object, _, _ := types.LookupFieldOrMethod(pointer, true, method.Pkg(), method.Name())
		if implementation, ok := object.(*types.Func); ok {
			result = append(result, inventoryObjectID(implementation))
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func attachPortConsumers(ports []PortInventory, functions []FunctionInventory) {
	uses := make(map[string][]PortConsumer)
	for _, function := range functions {
		for _, call := range function.Calls {
			uses[call.Symbol] = append(uses[call.Symbol], PortConsumer{
				Symbol: function.Symbol, File: function.File, Line: call.Line, Target: call.Symbol,
			})
		}
	}
	for index := range ports {
		port := &ports[index]
		port.Consumers = append(port.Consumers, uses[port.Symbol]...)
		for _, method := range port.ImplementationMethods {
			port.Consumers = append(port.Consumers, uses[method]...)
		}
		slices.SortFunc(port.Consumers, func(left, right PortConsumer) int {
			if comparison := strings.Compare(left.File, right.File); comparison != 0 {
				return comparison
			}
			if left.Line != right.Line {
				return left.Line - right.Line
			}
			return strings.Compare(left.Target, right.Target)
		})
		port.Consumers = slices.Compact(port.Consumers)
	}
}

// mergePortBuilds retains implementations found only in an additional supported build.
func mergePortBuilds(ports []PortInventory) []PortInventory {
	key := func(port PortInventory) string {
		return port.Symbol + "\\x00" + port.Signature + "\\x00" + port.File
	}
	slices.SortFunc(ports, func(left, right PortInventory) int {
		return strings.Compare(key(left), key(right))
	})
	result := make([]PortInventory, 0, len(ports))
	for _, port := range ports {
		if len(result) == 0 || key(result[len(result)-1]) != key(port) {
			result = append(result, port)
			continue
		}
		target := &result[len(result)-1]
		target.Repositories = mergePortStrings(target.Repositories, port.Repositories)
		target.RepositoryMethods = mergePortStrings(target.RepositoryMethods, port.RepositoryMethods)
		target.Implementations = mergePortStrings(target.Implementations, port.Implementations)
		target.ImplementationMethods = mergePortStrings(target.ImplementationMethods, port.ImplementationMethods)
		target.ArchiveResources = append(target.ArchiveResources, port.ArchiveResources...)
	}
	return result
}

func mergePortStrings(left, right []string) []string {
	result := append(slices.Clone(left), right...)
	slices.Sort(result)
	return slices.Compact(result)
}
