package libraryimport

import (
	"errors"
	"testing"

	"retrom/internal/content/diagnostic"
)

func TestOwnedImportPreservesPrimaryRejectionBeforeGroupValidation(t *testing.T) {
	t.Parallel()
	expected := diagnostic.Rejection{
		Code: "MULTI_DISC_TOTAL_BYTES_EXCEEDED", RelativePath: "game.m3u",
		Limit: &diagnostic.Limit{Metric: "TOTAL_BYTES", Actual: 1436977078, Maximum: 1073741824},
	}
	plan := PreparedImport{Dispositions: []PreparedDisposition{
		{File: ImportFile{Path: "unused.chd"}, Disposition: "IGNORED", Reason: "IGNORED_UNREFERENCED_FILE"},
		{File: ImportFile{Path: "game.m3u"}, Disposition: "REJECTED", Reason: expected.Code, Rejection: &expected},
	}}
	source := &OwnedImportCreation{Intent: SourceCreationIntent{PrimaryPaths: []string{"game.m3u", "a.chd", "b.chd"}}}
	err := validateOwnedImportPlan(plan, source)
	var rejected *ContentRejectedError
	if !errors.As(err, &rejected) || rejected.Rejection.Code != expected.Code || rejected.Rejection.Limit != expected.Limit {
		t.Fatalf("rejection replaced by grouping error: %+v", err)
	}
	source.Before.TargetVersion++
	if err := validateOwnedImportPlan(plan, source); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale target lost execution fence: %v", err)
	}
}
