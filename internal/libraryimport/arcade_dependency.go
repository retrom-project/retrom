package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

const maxArcadeDependencyNodes = 64

type arcadeMachineRelation struct {
	cloneOf string
	romOf   string
}

type arcadeClosureNode struct {
	Machine    string  `json:"machine"`
	Kind       string  `json:"kind"`
	RequiredBy *string `json:"requiredBy"`
	Depth      int     `json:"depth"`
}

type arcadeRelationResolver func(machine string) (arcadeMachineRelation, bool)

// arcadeDependencyClosureV2 is deliberately independent of storage. Callers
// provide the locked DAT relation resolver; the returned order is canonical.
//
//nolint:gocognit,gocyclo,nestif // Bounded traversal keeps cycle, romof, cloneof, and the 64-node guard together.
func arcadeDependencyClosureV2(
	machine string,
	resolve arcadeRelationResolver,
) ([]arcadeClosureNode, bool, bool) {
	if machine == "" {
		return nil, false, false
	}
	nodes := []arcadeClosureNode{{Machine: machine, Kind: "CONTENT", Depth: 0}}
	index := map[string]int{machine: 0}
	chain := make(map[string]struct{})
	current := machine
	depth := 0
	for current != "" {
		if _, exists := chain[current]; exists {
			return nil, true, true
		}
		chain[current] = struct{}{}
		relation, exists := resolve(current)
		if !exists {
			return nil, false, false
		}
		if relation.romOf != "" && relation.romOf != relation.cloneOf {
			if _, exists := resolve(relation.romOf); !exists {
				return nil, false, false
			}
			if _, exists := index[relation.romOf]; !exists {
				if len(nodes) >= maxArcadeDependencyNodes {
					return nil, true, true
				}
				requiredBy := current
				index[relation.romOf] = len(nodes)
				nodes = append(nodes, arcadeClosureNode{
					Machine: relation.romOf, Kind: "BIOS_OR_BASE", RequiredBy: &requiredBy, Depth: depth + 1,
				})
			}
		}
		if relation.cloneOf == "" {
			break
		}
		if _, exists := chain[relation.cloneOf]; exists {
			return nil, true, true
		}
		if _, exists := resolve(relation.cloneOf); !exists {
			return nil, false, false
		}
		requiredBy := current
		if existing, exists := index[relation.cloneOf]; exists {
			nodes[existing] = arcadeClosureNode{
				Machine: relation.cloneOf, Kind: "PARENT", RequiredBy: &requiredBy, Depth: depth + 1,
			}
		} else {
			if len(nodes) >= maxArcadeDependencyNodes {
				return nil, true, true
			}
			index[relation.cloneOf] = len(nodes)
			nodes = append(nodes, arcadeClosureNode{
				Machine: relation.cloneOf, Kind: "PARENT", RequiredBy: &requiredBy, Depth: depth + 1,
			})
		}
		current = relation.cloneOf
		depth++
	}
	sort.Slice(nodes, func(left, right int) bool {
		if nodes[left].Depth != nodes[right].Depth {
			return nodes[left].Depth < nodes[right].Depth
		}
		leftKind := arcadeClosureKindOrder(nodes[left].Kind)
		rightKind := arcadeClosureKindOrder(nodes[right].Kind)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		return nodes[left].Machine < nodes[right].Machine
	})
	return nodes, false, true
}

func arcadeClosureKindOrder(kind string) int {
	switch kind {
	case "CONTENT":
		return 0
	case "PARENT":
		return 1
	default:
		return 2
	}
}

func (service *Service) loadArcadeDependencyClosure(
	ctx context.Context,
	datID, machine string,
) ([]arcadeClosureNode, bool, error) {
	return loadArcadeDependencyClosure(ctx, service.database, datID, machine)
}

type arcadeRelationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadArcadeDependencyClosure(
	ctx context.Context,
	queryer arcadeRelationQueryer,
	datID, machine string,
) ([]arcadeClosureNode, bool, error) {
	cache := make(map[string]arcadeMachineRelation)
	missing := make(map[string]struct{})
	resolve := func(name string) (arcadeMachineRelation, bool) {
		if relation, exists := cache[name]; exists {
			return relation, true
		}
		if _, absent := missing[name]; absent {
			return arcadeMachineRelation{}, false
		}
		var cloneOf, romOf sql.NullString
		err := queryer.QueryRowContext(ctx, `
SELECT cloneof,romof
FROM dat_machines
WHERE dat_version_id=? AND machine_name=?
`, datID, name).Scan(&cloneOf, &romOf)
		if errors.Is(err, sql.ErrNoRows) {
			missing[name] = struct{}{}
			return arcadeMachineRelation{}, false
		}
		if err != nil {
			missing[name] = struct{}{}
			return arcadeMachineRelation{}, false
		}
		relation := arcadeMachineRelation{cloneOf: cloneOf.String, romOf: romOf.String}
		cache[name] = relation
		return relation, true
	}
	nodes, cyclic, available := arcadeDependencyClosureV2(machine, resolve)
	if !available && ctx.Err() != nil {
		return nil, false, fmt.Errorf("libraryimport/arcade dependency: %w", ctx.Err())
	}
	if !available {
		return nil, false, sql.ErrNoRows
	}
	return nodes, cyclic, nil
}
