package arcade

import (
	"context"
	"sort"
)

func CanonicalSnapshot(
	ctx context.Context,
	reader RelationReader,
	raw string,
) (Snapshot, error) {
	snapshot, valid := ParseSnapshot(raw)
	if !valid {
		return Snapshot{}, ErrInvalid
	}
	nodes, cyclic, err := LoadClosure(ctx, reader, snapshot.DatVersionID, snapshot.Machine)
	if err != nil {
		return Snapshot{}, err
	}
	if cyclic {
		return Snapshot{}, ErrInvalid
	}
	byMachine := make(map[string]ClosureNode, len(nodes))
	for _, node := range nodes {
		byMachine[node.Machine] = node
	}
	for index := range snapshot.Dependencies {
		dependency := &snapshot.Dependencies[index]
		node, exists := byMachine[dependency.Machine]
		if !exists || node.Kind != dependency.Kind {
			return Snapshot{}, ErrInvalid
		}
		dependency.RequiredBy = node.RequiredBy
		dependency.Depth = node.Depth
		dependency.ExpectedLogicalName = dependency.Machine + ".zip"
		dependency.RequiredEntryCount = len(dependency.RequiredEntries)
	}
	snapshot.Closure = nodes
	sort.Slice(snapshot.Dependencies, func(left, right int) bool {
		if snapshot.Dependencies[left].Kind != snapshot.Dependencies[right].Kind {
			return snapshot.Dependencies[left].Kind < snapshot.Dependencies[right].Kind
		}
		if snapshot.Dependencies[left].Depth != snapshot.Dependencies[right].Depth {
			return snapshot.Dependencies[left].Depth < snapshot.Dependencies[right].Depth
		}
		return snapshot.Dependencies[left].Machine < snapshot.Dependencies[right].Machine
	})
	return snapshot, nil
}
