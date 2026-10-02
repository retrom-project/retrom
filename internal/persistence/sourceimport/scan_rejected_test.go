package sourceimport

import (
	"testing"
	"time"

	application "retrom/internal/service/sourceimport"
)

func TestScanWithOnlyInvalidMetadataCannotEnterMapping(t *testing.T) {
	t.Parallel()
	db, id, projection := publicationDatabase(t)
	projection.Headers.Metadata[0].State = "INVALID"
	projection.Headers.Metadata[0].ErrorCode = "PEGASUS_METADATA_SYNTAX_INVALID"
	projection.Items = nil
	projection.Headers.Collections = nil
	projection.Summary.Shape = application.ScanShape{Metadata: 1, InvalidMetadata: 1}
	service := application.NewScanPublication(NewScanPublication(db), func() time.Time { return time.UnixMilli(10) })
	if err := service.Save(t.Context(), id, projection); err != nil {
		t.Fatal(err)
	}
	summary, err := NewQueries(db).Get(t.Context(), id.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.State != "FAILED" || summary.CompletedAtMS == nil {
		t.Fatalf("invalid metadata must expose a recoverable failure, got %s", summary.State)
	}
}
