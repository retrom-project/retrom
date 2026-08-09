package libraryimport

import (
	"fmt"
	"slices"
	"testing"
)

func TestArcadeDependencyClosureV2BuildsMultilevelParentAndBaseRelations(t *testing.T) {
	t.Parallel()
	relations := map[string]arcadeMachineRelation{
		"a": {cloneOf: "b", romOf: "base-a"}, "b": {cloneOf: "c", romOf: "base-b"},
		"c": {romOf: "bios-x"}, "base-a": {}, "base-b": {}, "bios-x": {},
	}
	nodes, cyclic, available := arcadeDependencyClosureV2("a", relationMapResolver(relations))
	if cyclic || !available {
		t.Fatalf("closure flags = cyclic:%v available:%v", cyclic, available)
	}
	got := make([]string, 0, len(nodes))
	for _, node := range nodes {
		requiredBy := "-"
		if node.RequiredBy != nil {
			requiredBy = *node.RequiredBy
		}
		got = append(got, fmt.Sprintf("%s:%s:%s:%d", node.Machine, node.Kind, requiredBy, node.Depth))
	}
	want := []string{
		"a:CONTENT:-:0", "b:PARENT:a:1", "base-a:BIOS_OR_BASE:a:1",
		"c:PARENT:b:2", "base-b:BIOS_OR_BASE:b:2", "bios-x:BIOS_OR_BASE:c:3",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("closure = %#v, want %#v", got, want)
	}
}

func TestArcadeDependencyClosureV2RejectsCyclesMissingRelationsAndOverflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		relations map[string]arcadeMachineRelation
		cycle     bool
		available bool
	}{
		{name: "self", relations: map[string]arcadeMachineRelation{"a": {cloneOf: "a"}}, cycle: true, available: true},
		{name: "two nodes", relations: map[string]arcadeMachineRelation{"a": {cloneOf: "b"}, "b": {cloneOf: "a"}}, cycle: true, available: true},
		{name: "missing", relations: map[string]arcadeMachineRelation{"a": {cloneOf: "missing"}}, available: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, cyclic, available := arcadeDependencyClosureV2("a", relationMapResolver(test.relations))
			if cyclic != test.cycle || available != test.available {
				t.Fatalf("flags = cyclic:%v available:%v", cyclic, available)
			}
		})
	}
	overflow := make(map[string]arcadeMachineRelation)
	for index := 0; index < maxArcadeDependencyNodes; index++ {
		name := fmt.Sprintf("m%02d", index)
		next := ""
		if index+1 < maxArcadeDependencyNodes {
			next = fmt.Sprintf("m%02d", index+1)
		}
		base := fmt.Sprintf("bios%02d", index)
		overflow[name] = arcadeMachineRelation{cloneOf: next, romOf: base}
		overflow[base] = arcadeMachineRelation{}
	}
	if _, cyclic, available := arcadeDependencyClosureV2("m00", relationMapResolver(overflow)); !cyclic || !available {
		t.Fatalf("overflow flags = cyclic:%v available:%v", cyclic, available)
	}
}

func TestArcadeDependencyClosureV2IsCanonicalAcrossMapConstructionOrder(t *testing.T) {
	t.Parallel()
	first := map[string]arcadeMachineRelation{
		"a": {cloneOf: "b", romOf: "bios"}, "b": {}, "bios": {},
	}
	second := make(map[string]arcadeMachineRelation)
	second["bios"], second["b"] = arcadeMachineRelation{}, arcadeMachineRelation{}
	second["a"] = arcadeMachineRelation{cloneOf: "b", romOf: "bios"}
	left, _, _ := arcadeDependencyClosureV2("a", relationMapResolver(first))
	right, _, _ := arcadeDependencyClosureV2("a", relationMapResolver(second))
	if !slices.EqualFunc(left, right, func(a, b arcadeClosureNode) bool {
		parentA, parentB := "", ""
		if a.RequiredBy != nil {
			parentA = *a.RequiredBy
		}
		if b.RequiredBy != nil {
			parentB = *b.RequiredBy
		}
		return a.Machine == b.Machine && a.Kind == b.Kind && a.Depth == b.Depth && parentA == parentB
	}) {
		t.Fatal("closure changed with map construction order")
	}
}

func relationMapResolver(relations map[string]arcadeMachineRelation) arcadeRelationResolver {
	return func(machine string) (arcadeMachineRelation, bool) {
		relation, exists := relations[machine]
		return relation, exists
	}
}
