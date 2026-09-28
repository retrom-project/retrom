package libraryimport

import libraryservice "retrom/internal/service/libraryimport"

const maxArcadeDependencyNodes = libraryservice.MaxArcadeDependencyNodes

type (
	arcadeMachineRelation  struct{ cloneOf, romOf string }
	arcadeClosureNode      = libraryservice.ArcadeClosureNode
	arcadeRelationResolver func(string) (arcadeMachineRelation, bool)
)

func arcadeDependencyClosureV2(
	machine string,
	resolve arcadeRelationResolver,
) ([]arcadeClosureNode, bool, bool) {
	return libraryservice.ArcadeDependencyClosure(machine, func(name string) (libraryservice.ArcadeMachineRelation, bool) {
		relation, found := resolve(name)
		return libraryservice.ArcadeMachineRelation{CloneOf: relation.cloneOf, ROMOf: relation.romOf}, found
	})
}
