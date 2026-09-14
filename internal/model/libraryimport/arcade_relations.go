package libraryimport

import "context"

type ArcadeRelationReader interface {
	MachineRelation(context.Context, string, string) (ArcadeMachineRelation, bool, error)
}
