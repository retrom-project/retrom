package storageanalysis

import (
	"errors"
	"testing"

	"retrom/internal/blobregistry"
)

func TestReferenceCoverageRejectsNewAndStaleEdges(t *testing.T) {
	edges, err := blobregistry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReferenceCoverage(edges); err != nil {
		t.Fatalf("current registry coverage: %v", err)
	}
	withNewEdge := append(append([]blobregistry.Edge(nil), edges...), blobregistry.Edge{
		Table: "future_table", Column: "blob_id", Target: "BLOBS", Class: "PROTECTIVE",
	})
	if err := validateReferenceCoverage(withNewEdge); !errors.Is(err, errReferenceCoverage) {
		t.Fatalf("new edge error = %v", err)
	}
	withoutEdge := make([]blobregistry.Edge, 0, len(edges)-1)
	for _, edge := range edges {
		if edge.Table != "upload_files" || edge.Column != "final_blob_id" {
			withoutEdge = append(withoutEdge, edge)
		}
	}
	if err := validateReferenceCoverage(withoutEdge); !errors.Is(err, errReferenceCoverage) {
		t.Fatalf("stale capacity mapping error = %v", err)
	}
}
