package library

import (
	"errors"
	"testing"

	"retrom/internal/model"
)

func TestParentPruningCannotRetireAnImmutablePackageMember(t *testing.T) {
	files := []model.ContentFile{{LogicalKey: "parent.zip", Package: &model.GameFile{ID: "physical-owner"}}}
	if _, _, err := pruneContent(files, map[string]bool{"parent.zip": true}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("allowed an independent member retirement: %v", err)
	}
	files[0].Package = nil
	files[0].ID = "standalone-parent"
	retained, retired, err := pruneContent(files, map[string]bool{"parent.zip": true})
	if err != nil || len(retained) != 0 || len(retired) != 1 || retired[0] != "standalone-parent" {
		t.Fatalf("native parent lifecycle changed: %v", err)
	}
}
