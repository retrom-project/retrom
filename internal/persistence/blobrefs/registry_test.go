package blobrefs

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"retrom/internal/persistence/blobregistry"
)

func TestExplicitCountersExactlyCoverProtectiveRegistryEdges(t *testing.T) {
	edges, err := blobregistry.Load()
	if err != nil {
		t.Fatal(err)
	}
	var want, got []string
	for _, edge := range edges {
		if edge.Class == "PROTECTIVE" {
			want = append(want, edge.Table+"."+edge.Column)
		}
	}
	for table, fields := range columns {
		if table == "archive_entries" {
			continue // Archive membership is conditional, covered by the transition tests.
		}
		for _, field := range strings.Split(fields, ",") {
			got = append(got, table+"."+field)
		}
	}
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit counters=%v; registry=%v", got, want)
	}
}
