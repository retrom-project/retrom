package pegasusimport

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	repository "retrom/internal/repo/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

type handoffStoredState struct {
	Metadata, Search, State, Warnings                                 string
	DraftVersion, ItemVersion, ParentVersion, Pending, Events, Audits int64
}

func readHandoffState(t *testing.T, service *Service) handoffStoredState {
	t.Helper()
	var result handoffStoredState
	if err := service.database.QueryRowContext(t.Context(), `SELECT d.metadata_json,d.version,i.search_text,p.execution_state,p.warnings_json,p.version,
 parent.version,parent.review_pending_item_count,(SELECT count(*) FROM job_events),(SELECT count(*) FROM review_events)
 FROM review_drafts d JOIN import_items i ON i.id=d.import_item_id
 JOIN pegasus_import_items p ON p.library_import_item_id=i.id JOIN pegasus_imports parent ON parent.id=p.import_id
 WHERE p.id='item'`).Scan(&result.Metadata, &result.DraftVersion, &result.Search, &result.State, &result.Warnings, &result.ItemVersion,
		&result.ParentVersion, &result.Pending, &result.Events, &result.Audits); err != nil {
		t.Fatal(err)
	}
	return result
}

func handoffRequest(unit work) pegasusimportmodel.ReviewHandoffRequest {
	return pegasusimportmodel.ReviewHandoffRequest{ItemID: "item", ImportID: unit.ImportID, JobID: unit.JobID, LibraryJobID: "handoff-job", LibraryItemID: "handoff-item", ExecutionNo: unit.ExecutionNo, Attempt: unit.Attempt, WorkerID: unit.WorkerID}
}

func TestReviewHandoffRejectsStaleIdentity(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"execution", "attempt", "worker", "library"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			service, unit, _ := handoffFixture(t)
			before := readHandoffState(t, service)
			request := handoffRequest(unit)
			switch field {
			case "execution":
				request.ExecutionNo++
			case "attempt":
				request.Attempt++
			case "worker":
				request.WorkerID = "other-worker"
			case "library":
				request.LibraryJobID = "foreign"
			}
			repo := repository.NewReviewHandoff(service.database)
			label := "release-setup"
			err := repo.CommitReviewHandoff(t.Context(), request, service.now().UnixMilli(),
				"audit-id", "SYSTEM", nil, &label, service.now().UTC().Year()+1)
			if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
				t.Fatalf("stale %s error=%v", field, err)
			}
			if after := readHandoffState(t, service); !reflect.DeepEqual(after, before) {
				t.Fatalf("%s partially committed", field)
			}
		})
	}
}

func TestReviewHandoffCommitsMetadataWarningsCountsAndEventOnce(t *testing.T) {
	t.Parallel()
	service, unit, _ := handoffFixture(t)
	metadata := `{"Title":"Changed","Developer":"` + strings.Repeat("开", 201) + `"}`
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE pegasus_import_items SET metadata_json=?,warnings_json='[{"code":"SOURCE_WARNING","field":"file"}]' WHERE id='item'`, metadata)
	before := readHandoffState(t, service)
	handoff := pegasusimportservice.NewReviewHandoff(repository.NewReviewHandoff(service.database), nil, service.now)
	request := handoffRequest(unit)
	if err := handoff.Complete(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	after := readHandoffState(t, service)
	if after.State != "REVIEW_PENDING" || after.DraftVersion != before.DraftVersion+1 || after.ItemVersion != before.ItemVersion+1 || after.ParentVersion != before.ParentVersion+1 || after.Pending != 1 || after.Events != before.Events+1 || after.Audits != before.Audits+1 {
		t.Fatalf("handoff projections: before=%#v after=%#v", before, after)
	}
	expected := `[{"code":"SOURCE_WARNING","field":"file"},{"code":"FIELD_TRUNCATED","field":"developer"}]`
	if after.Warnings != expected {
		t.Fatalf("warnings=%s", after.Warnings)
	}
	if err := handoff.Complete(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if repeated := readHandoffState(t, service); !reflect.DeepEqual(repeated, after) {
		t.Fatalf("repeated handoff changed result: %#v", repeated)
	}
}
