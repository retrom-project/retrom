package sourceimport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	repository "retrom/internal/persistence/sourceimport"
	library "retrom/internal/service/libraryimport"
	application "retrom/internal/service/sourceimport"
)

type handoffTransactionFailure struct {
	repository application.ReviewHandoffRepository
	stage      string
	cause      error
}

func (failure handoffTransactionFailure) WithReviewHandoff(ctx context.Context, work func(application.ReviewHandoffScope) error) error {
	return failure.repository.WithReviewHandoff(ctx, func(scope application.ReviewHandoffScope) error {
		scope.Records = handoffWriteFailure{ReviewHandoffRecords: scope.Records, stage: failure.stage, cause: failure.cause}
		return work(scope)
	})
}

type handoffWriteFailure struct {
	application.ReviewHandoffRecords
	stage string
	cause error
}

func (failure handoffWriteFailure) FinishReviewHandoff(ctx context.Context, change application.ReviewHandoffChange) error {
	switch failure.stage {
	case "item":
		change.Before.Version++
	case "parent":
		change.Before.ImportVersion++
	case "execution":
		change.Before.Identity.ExecutionNo++
	case "attempt":
		change.Before.Identity.Attempt++
	case "library":
		change.Before.Identity.LibraryJobID = "foreign"
	}
	if err := failure.ReviewHandoffRecords.FinishReviewHandoff(ctx, change); err != nil {
		return err
	}
	return failure.cause
}

type handoffStoredState struct {
	Metadata, Search, State, Warnings                         string
	DraftVersion, ItemVersion, ParentVersion, Pending, Events int64
}

func readHandoffState(t *testing.T, service *Service) handoffStoredState {
	t.Helper()
	var result handoffStoredState
	if err := service.database.QueryRowContext(t.Context(), `SELECT d.metadata_json,d.review_version,i.search_text,p.execution_state,p.warnings_json,p.version,
 parent.version,parent.review_pending_item_count,(SELECT count(*) FROM job_events)
 FROM import_items d JOIN import_items i ON i.id=d.id
 JOIN source_import_items p ON p.library_import_item_id=i.id JOIN source_imports parent ON parent.id=p.import_id
 WHERE p.id='item'`).Scan(&result.Metadata, &result.DraftVersion, &result.Search, &result.State, &result.Warnings, &result.ItemVersion,
		&result.ParentVersion, &result.Pending, &result.Events); err != nil {
		t.Fatal(err)
	}
	return result
}

func handoffRequest(unit work) application.ReviewHandoffRequest {
	return application.ReviewHandoffRequest{ItemID: "item", ImportID: unit.ImportID, JobID: unit.JobID, LibraryJobID: "handoff-job", LibraryItemID: "handoff-item", ExecutionNo: unit.ExecutionNo, Attempt: unit.Attempt, WorkerID: unit.WorkerID}
}

func TestReviewHandoffTransactionRollsBackEveryProjection(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"item", "parent", "execution", "attempt", "library", "late callback"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			service, unit, _ := handoffFixture(t)
			before := readHandoffState(t, service)
			cause := errors.New("late handoff failure")
			storage := handoffTransactionFailure{repository: repository.NewReviewHandoff(service.database), stage: stage, cause: cause}
			handoff := application.NewReviewHandoff(storage, library.NewMetadataSeeder(nil, service.now), service.now)
			err := handoff.Complete(t.Context(), handoffRequest(unit))
			if err == nil || stage == "late callback" && !errors.Is(err, cause) {
				t.Fatalf("failed %s handoff: %v", stage, err)
			}
			if after := readHandoffState(t, service); !reflect.DeepEqual(after, before) {
				t.Fatalf("%s partially committed: before=%#v after=%#v", stage, before, after)
			}
		})
	}
}

func TestReviewHandoffCommitsMetadataWarningsCountsAndEventOnce(t *testing.T) {
	t.Parallel()
	service, unit, _ := handoffFixture(t)
	metadata := `{"Title":"Changed","Developer":"` + strings.Repeat("开", 201) + `"}`
	mustExecSourceTest(t.Context(), t, service.database, `UPDATE source_import_items SET metadata_json=?,warnings_json='[{"code":"SOURCE_WARNING","field":"file"}]' WHERE id='item'`, metadata)
	before := readHandoffState(t, service)
	handoff := application.NewReviewHandoff(repository.NewReviewHandoff(service.database), library.NewMetadataSeeder(nil, service.now), service.now)
	request := handoffRequest(unit)
	if err := handoff.Complete(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	after := readHandoffState(t, service)
	if after.State != "REVIEW_PENDING" || after.DraftVersion != before.DraftVersion+1 || after.ItemVersion != before.ItemVersion+1 || after.ParentVersion != before.ParentVersion+1 || after.Pending != 1 || after.Events != before.Events+1 {
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
