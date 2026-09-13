package libraryimport

import application "retrom/internal/service/libraryimport"

const maxArcadeDependencyNodes = application.MaxArcadeDependencyNodes

type (
	arcadeMachineRelation  struct{ cloneOf, romOf string }
	arcadeClosureNode      = application.ArcadeClosureNode
	arcadeRelationResolver func(string) (arcadeMachineRelation, bool)
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
