//go:build integration

package libraryimport

import (
	"encoding/json"
	"errors"
	"testing"

	"retrom/internal/capability/security/authn"
	libraryimportmodel "retrom/internal/model/libraryimport"
	taggingmodel "retrom/internal/model/tagging"
	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func TestImportAdmissionFreezesValidatedTagsAndActor(t *testing.T) {
	t.Parallel()
	service, request := admissionFixture(t)
	const actor = "01980000-0000-7000-8000-000000001011"
	if _, err := service.database.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('admission-profile','Admission',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.database.ExecContext(t.Context(), `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES(?,'admission-profile','admission','Admission','ADMIN','ENABLED',1,1)`, actor); err != nil {
		t.Fatal(err)
	}
	tag, err := service.tags.Create(t.Context(), actor, "Import tag")
	if err != nil {
		t.Fatal(err)
	}
	request.TagIDs = []string{tag.TagID}
	admissions := application.NewImportAdmissions(repository.NewImportAdmissions(service.database), nil, service.tags,
		application.ImportAdmissionOptions{Now: service.now})
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: actor})
	result, err := admissions.Queue(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var actorID, document string
	if err := service.database.QueryRowContext(ctx, `SELECT actor_user_id,request_json FROM import_group_requests WHERE import_job_id=?`, result.ImportJobID).Scan(&actorID, &document); err != nil {
		t.Fatal(err)
	}
	var frozen libraryimportmodel.QueuedImportRequest
	if err := json.Unmarshal([]byte(document), &frozen); err != nil {
		t.Fatal(err)
	}
	if actorID != actor || len(frozen.Tags) != 1 || frozen.Tags[0].TagID != tag.TagID || len(frozen.Request.TagIDs) != 1 || frozen.Request.TagIDs[0] != tag.TagID {
		t.Fatalf("actor=%s frozen=%+v", actorID, frozen)
	}
	request.TagIDs = []string{"01980000-0000-7000-8000-000000001012"}
	result, err = admissions.Queue(ctx, request)
	if !errors.Is(err, taggingmodel.ErrReferenceInvalid) || result != (Created{}) {
		t.Fatalf("invalid tag result=%+v err=%v", result, err)
	}
}
