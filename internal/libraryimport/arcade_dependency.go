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
// Bounded traversal keeps cycle, romof, cloneof, and the 64-node guard together.
func arcadeDependencyClosureV2(
	machine string,
	resolve arcadeRelationResolver,
) ([]arcadeClosureNode, bool, bool) {
	if machine == "" {
		return nil, false, false
	}
	traversal := arcadeClosureTraversal{
		resolve: resolve,
		nodes:   []arcadeClosureNode{{Machine: machine, Kind: "CONTENT", Depth: 0}},
		index:   map[string]int{machine: 0},
		chain:   make(map[string]struct{}),
		current: machine,
	}
	for traversal.current != "" {
		done, cyclic, available := traversal.step()
		if done {
			return nil, cyclic, available
		}
	}
	traversal.sort()
	return traversal.nodes, false, true
}

type arcadeClosureTraversal struct {
	resolve arcadeRelationResolver
	nodes   []arcadeClosureNode
	index   map[string]int
	chain   map[string]struct{}
	current string
	depth   int
}

func (traversal *arcadeClosureTraversal) step() (bool, bool, bool) {
	if _, exists := traversal.chain[traversal.current]; exists {
		return true, true, true
	}
	traversal.chain[traversal.current] = struct{}{}
	relation, exists := traversal.resolve(traversal.current)
	if !exists {
		return true, false, false
	}
	if !traversal.appendROMDependency(relation) {
		return true, traversal.capacityExceeded(), traversal.capacityExceeded()
	}
	if relation.cloneOf == "" {
		traversal.current = ""
		return false, false, true
	}
	return traversal.advanceToParent(relation.cloneOf)
}

func (traversal *arcadeClosureTraversal) appendROMDependency(relation arcadeMachineRelation) bool {
	if relation.romOf == "" || relation.romOf == relation.cloneOf {
		return true
	}
	if _, exists := traversal.resolve(relation.romOf); !exists {
		return false
	}
	if _, exists := traversal.index[relation.romOf]; exists {
		return true
	}
	if len(traversal.nodes) >= maxArcadeDependencyNodes {
		return false
	}
	requiredBy := traversal.current
	traversal.index[relation.romOf] = len(traversal.nodes)
	traversal.nodes = append(traversal.nodes, arcadeClosureNode{
		Machine: relation.romOf, Kind: "BIOS_OR_BASE",
		RequiredBy: &requiredBy, Depth: traversal.depth + 1,
	})
	return true
}

func (traversal *arcadeClosureTraversal) advanceToParent(parent string) (bool, bool, bool) {
	if _, exists := traversal.chain[parent]; exists {
		return true, true, true
	}
	if _, exists := traversal.resolve(parent); !exists {
		return true, false, false
	}
	if !traversal.upsertParent(parent) {
		return true, true, true
	}
	traversal.current = parent
	traversal.depth++
	return false, false, true
}

func (traversal *arcadeClosureTraversal) upsertParent(parent string) bool {
	requiredBy := traversal.current
	node := arcadeClosureNode{
		Machine: parent, Kind: "PARENT", RequiredBy: &requiredBy, Depth: traversal.depth + 1,
	}
	if existing, exists := traversal.index[parent]; exists {
		traversal.nodes[existing] = node
		return true
	}
	if len(traversal.nodes) >= maxArcadeDependencyNodes {
		return false
	}
	traversal.index[parent] = len(traversal.nodes)
	traversal.nodes = append(traversal.nodes, node)
	return true
}

func (traversal *arcadeClosureTraversal) capacityExceeded() bool {
	return len(traversal.nodes) >= maxArcadeDependencyNodes
}

func (traversal *arcadeClosureTraversal) sort() {
	sort.Slice(traversal.nodes, func(left, right int) bool {
		if traversal.nodes[left].Depth != traversal.nodes[right].Depth {
			return traversal.nodes[left].Depth < traversal.nodes[right].Depth
		}
		leftKind := arcadeClosureKindOrder(traversal.nodes[left].Kind)
		rightKind := arcadeClosureKindOrder(traversal.nodes[right].Kind)
		if leftKind != rightKind {
			return leftKind < rightKind
		}
		return traversal.nodes[left].Machine < traversal.nodes[right].Machine
	})
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
