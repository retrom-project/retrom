//go:build integration

package libraryimport

import (
	"errors"
	"testing"

	application "retrom/internal/service/libraryimport"
)

func TestOwnedESSourceRejectsChangedFrozenRequest(t *testing.T) {
	for _, field := range []string{"actor", "tags", "content mode"} {
		t.Run(field, func(t *testing.T) {
			fixture, request := ownedESSourceFixture(t)
			switch field {
			case "actor":
				request.AssignedByUserID = "different-actor"
			case "tags":
				request.TagIDs = []string{"unexpected-tag"}
			case "content mode":
				request.ContentMode = "MULTI_DISC"
			}
			result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
			if !errors.Is(err, ErrInvalid) || result.Created.ImportJobID != "" || result.Items != nil {
				t.Fatalf("changed frozen %s accepted: result=%+v err=%v", field, result, err)
			}
			var imports, owners int
			if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT (SELECT count(*) FROM import_jobs),
   (SELECT count(*) FROM server_import_upload_owners WHERE kind='EMULATIONSTATION')`).Scan(&imports, &owners); err != nil {
				t.Fatal(err)
			}
			if imports != 0 || owners != 0 {
				t.Fatalf("changed request created imports=%d owners=%d", imports, owners)
			}
		})
	}
}

func TestOwnedSourceRequiresExplicitKindBeforeDatabaseAccess(t *testing.T) {
	service := &Service{}
	for _, kind := range []application.SourceOwnerKind{"", "other"} {
		intent := application.SourceCreationIntent{Kind: kind, ImportID: "plan", ItemID: "source"}
		result, found, err := service.LookupOwnedServerSource(t.Context(), intent)
		if !errors.Is(err, ErrInvalid) || found || result.Created.ImportJobID != "" {
			t.Fatalf("kind=%q found=%v result=%+v err=%v", kind, found, result, err)
		}
	}
}
