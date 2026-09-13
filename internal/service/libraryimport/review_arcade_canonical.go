package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

func CanonicalArcadeSnapshot(
	ctx context.Context,
	reader ArcadeRelationReader,
	raw string,
) (ArcadeDraftSnapshot, error) {
	snapshot, valid := ParseArcadeDraftSnapshot(raw)
	if !valid {
		return ArcadeDraftSnapshot{}, ErrInvalid
	}
	nodes, cyclic, err := LoadArcadeClosure(ctx, reader, snapshot.DatVersionID, snapshot.Machine)
	if err != nil {
		return ArcadeDraftSnapshot{}, err
	}
	if cyclic {
		return ArcadeDraftSnapshot{}, ErrInvalid
	}
	byMachine := make(map[string]ArcadeClosureNode, len(nodes))
	for _, node := range nodes {
		byMachine[node.Machine] = node
	}
	for index := range snapshot.Dependencies {
		dependency := &snapshot.Dependencies[index]
		node, exists := byMachine[dependency.Machine]
		if !exists || node.Kind != dependency.Kind {
			return ArcadeDraftSnapshot{}, ErrInvalid
		}
		dependency.RequiredBy = node.RequiredBy
		dependency.Depth = node.Depth
		dependency.ExpectedLogicalName = dependency.Machine + ".zip"
		dependency.RequiredEntryCount = len(dependency.RequiredEntries)
	}
	closure, err := json.Marshal(nodes)
	if err != nil {
		return ArcadeDraftSnapshot{}, fmt.Errorf("project arcade snapshot: %w", err)
	}
	snapshot.Closure = closure
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
