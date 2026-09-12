package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/dbexec"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

const maxArcadeDependencyNodes = application.MaxArcadeDependencyNodes

type (
	arcadeMachineRelation  struct{ cloneOf, romOf string }
	arcadeClosureNode      = application.ArcadeClosureNode
	arcadeRelationResolver func(string) (arcadeMachineRelation, bool)
	arcadeRelationQueryer  = dbexec.Executor
)

func arcadeDependencyClosureV2(
	machine string,
	resolve arcadeRelationResolver,
) ([]arcadeClosureNode, bool, bool) {
	return application.ArcadeDependencyClosure(machine, func(name string) (application.ArcadeMachineRelation, bool) {
		relation, found := resolve(name)
		return application.ArcadeMachineRelation{CloneOf: relation.cloneOf, ROMOf: relation.romOf}, found
	})
}

func (service *Service) loadArcadeDependencyClosure(
	ctx context.Context,
	datID, machine string,
) ([]arcadeClosureNode, bool, error) {
	return loadArcadeDependencyClosure(ctx, service.database, datID, machine)
}

func loadArcadeDependencyClosure(
	ctx context.Context,
	executor dbexec.Executor,
	datID, machine string,
) ([]arcadeClosureNode, bool, error) {
	nodes, cyclic, err := application.LoadArcadeClosure(ctx, repository.BindArcadeRelations(executor), datID, machine)
	if err != nil {
		return nil, false, fmt.Errorf("read arcade dependency closure: %w", err)
	}
	return nodes, cyclic, nil
}
