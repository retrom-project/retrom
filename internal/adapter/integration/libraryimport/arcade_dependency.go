package libraryimport

import (
	libraryimportmodel "retrom/internal/model/libraryimport"
)

const maxArcadeDependencyNodes = libraryimportmodel.MaxArcadeDependencyNodes

type (
	arcadeMachineRelation  struct{ cloneOf, romOf string }
	arcadeClosureNode      = libraryimportmodel.ArcadeClosureNode
	arcadeRelationResolver func(string) (arcadeMachineRelation, bool)
)

func arcadeDependencyClosureV2(
	machine string,
	resolve arcadeRelationResolver,
) ([]arcadeClosureNode, bool, bool) {
	return libraryimportmodel.ArcadeDependencyClosure(machine, func(name string) (
		libraryimportmodel.ArcadeMachineRelation,
		bool,
	) {
		relation, found := resolve(name)
		return libraryimportmodel.ArcadeMachineRelation{CloneOf: relation.cloneOf, ROMOf: relation.romOf}, found
	})
}
