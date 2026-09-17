package libraryimport

import (
	"context"
	"fmt"
	model "retrom/internal/model/libraryimport"
)

// LoadArcadeClosure coordinates relation reads for the application layer. The
// model package owns only the pure closure algorithm and its relation port.
func LoadArcadeClosure(
	ctx context.Context,
	reader model.ArcadeRelationReader,
	datID, machine string,
) ([]model.ArcadeClosureNode, bool, error) {
	cache := make(map[string]model.ArcadeMachineRelation)
	missing := make(map[string]bool)
	var failure error
	resolve := func(name string) (model.ArcadeMachineRelation, bool) {
		if failure != nil || missing[name] {
			return model.ArcadeMachineRelation{}, false
		}
		if relation, exists := cache[name]; exists {
			return relation, true
		}
		relation, found, err := reader.MachineRelation(ctx, datID, name)
		if err != nil {
			failure = err
			return model.ArcadeMachineRelation{}, false
		}
		if !found {
			missing[name] = true
			return model.ArcadeMachineRelation{}, false
		}
		cache[name] = relation
		return relation, true
	}
	nodes, cyclic, available := model.ArcadeDependencyClosure(machine, resolve)
	if failure != nil {
		return nil, false, fmt.Errorf("read arcade dependency relation: %w", failure)
	}
	if !available {
		return nil, false, model.ErrInvalid
	}
	return nodes, cyclic, nil
}
