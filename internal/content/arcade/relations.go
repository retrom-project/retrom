package arcade

import (
	"context"
	"fmt"
)

type RelationReader interface {
	MachineRelation(context.Context, string, string) (MachineRelation, bool, error)
}

func LoadClosure(
	ctx context.Context,
	reader RelationReader,
	datID, machine string,
) ([]ClosureNode, bool, error) {
	cache := make(map[string]MachineRelation)
	missing := make(map[string]bool)
	var failure error
	resolve := func(name string) (MachineRelation, bool) {
		if failure != nil || missing[name] {
			return MachineRelation{}, false
		}
		if relation, exists := cache[name]; exists {
			return relation, true
		}
		relation, found, err := reader.MachineRelation(ctx, datID, name)
		if err != nil {
			failure = err
			return MachineRelation{}, false
		}
		if !found {
			missing[name] = true
			return MachineRelation{}, false
		}
		cache[name] = relation
		return relation, true
	}
	nodes, cyclic, available := DependencyClosure(machine, resolve)
	if failure != nil {
		return nil, false, fmt.Errorf("read arcade dependency relation: %w", failure)
	}
	if !available {
		return nil, false, ErrInvalid
	}
	return nodes, cyclic, nil
}
