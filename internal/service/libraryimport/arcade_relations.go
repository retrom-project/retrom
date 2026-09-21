package libraryimport

import (
	"context"
	"fmt"
)

type ArcadeRelationReader interface {
	MachineRelation(context.Context, string, string) (ArcadeMachineRelation, bool, error)
}

func LoadArcadeClosure(
	ctx context.Context,
	reader ArcadeRelationReader,
	datID, machine string,
) ([]ArcadeClosureNode, bool, error) {
	cache := make(map[string]ArcadeMachineRelation)
	missing := make(map[string]bool)
	var failure error
	resolve := func(name string) (ArcadeMachineRelation, bool) {
		if failure != nil || missing[name] {
			return ArcadeMachineRelation{}, false
		}
		if relation, exists := cache[name]; exists {
			return relation, true
		}
		relation, found, err := reader.MachineRelation(ctx, datID, name)
		if err != nil {
			failure = err
			return ArcadeMachineRelation{}, false
		}
		if !found {
			missing[name] = true
			return ArcadeMachineRelation{}, false
		}
		cache[name] = relation
		return relation, true
	}
	nodes, cyclic, available := ArcadeDependencyClosure(machine, resolve)
	if failure != nil {
		return nil, false, fmt.Errorf("read arcade dependency relation: %w", failure)
	}
	if !available {
		return nil, false, ErrInvalid
	}
	return nodes, cyclic, nil
}
