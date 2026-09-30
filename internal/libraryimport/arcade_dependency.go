package libraryimport

import (
	"retrom/internal/content/arcade"
)

const maxArcadeDependencyNodes = arcade.MaxDependencyNodes

type (
	arcadeMachineRelation  struct{ cloneOf, romOf string }
	arcadeClosureNode      = arcade.ClosureNode
	arcadeRelationResolver func(string) (arcadeMachineRelation, bool)
)

func arcadeDependencyClosureV2(
	machine string,
	resolve arcadeRelationResolver,
) ([]arcadeClosureNode, bool, bool) {
	return arcade.DependencyClosure(machine, func(name string) (arcade.MachineRelation, bool) {
		relation, found := resolve(name)
		return arcade.MachineRelation{CloneOf: relation.cloneOf, ROMOf: relation.romOf}, found
	})
}
