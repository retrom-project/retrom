package libraryimport

import "sort"

const MaxArcadeDependencyNodes = 64

type ArcadeMachineRelation struct {
	CloneOf string
	ROMOf   string
}

type ArcadeClosureNode struct {
	Machine    string  `json:"machine"`
	Kind       string  `json:"kind"`
	RequiredBy *string `json:"requiredBy"`
	Depth      int     `json:"depth"`
}

type ArcadeRelationResolver func(machine string) (ArcadeMachineRelation, bool)

// ArcadeDependencyClosure is deliberately independent of storage. Callers
// provide the locked DAT relation resolver; the returned order is canonical.
//
// Bounded traversal keeps cycle, romof, cloneof, and the 64-node guard together.
func ArcadeDependencyClosure(machine string, resolve ArcadeRelationResolver) ([]ArcadeClosureNode, bool, bool) {
	if machine == "" {
		return nil, false, false
	}
	traversal := arcadeClosureTraversal{
		resolve: resolve,
		nodes:   []ArcadeClosureNode{{Machine: machine, Kind: "CONTENT", Depth: 0}},
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
	resolve ArcadeRelationResolver
	nodes   []ArcadeClosureNode
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
	if relation.CloneOf == "" {
		traversal.current = ""
		return false, false, true
	}
	return traversal.advanceToParent(relation.CloneOf)
}

func (traversal *arcadeClosureTraversal) appendROMDependency(relation ArcadeMachineRelation) bool {
	if relation.ROMOf == "" || relation.ROMOf == relation.CloneOf {
		return true
	}
	if _, exists := traversal.resolve(relation.ROMOf); !exists {
		return false
	}
	if _, exists := traversal.index[relation.ROMOf]; exists {
		return true
	}
	if len(traversal.nodes) >= MaxArcadeDependencyNodes {
		return false
	}
	requiredBy := traversal.current
	traversal.index[relation.ROMOf] = len(traversal.nodes)
	traversal.nodes = append(traversal.nodes, ArcadeClosureNode{
		Machine: relation.ROMOf, Kind: "BIOS_OR_BASE",
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
	node := ArcadeClosureNode{
		Machine: parent, Kind: "PARENT", RequiredBy: &requiredBy, Depth: traversal.depth + 1,
	}
	if existing, exists := traversal.index[parent]; exists {
		traversal.nodes[existing] = node
		return true
	}
	if len(traversal.nodes) >= MaxArcadeDependencyNodes {
		return false
	}
	traversal.index[parent] = len(traversal.nodes)
	traversal.nodes = append(traversal.nodes, node)
	return true
}

func (traversal *arcadeClosureTraversal) capacityExceeded() bool {
	return len(traversal.nodes) >= MaxArcadeDependencyNodes
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
