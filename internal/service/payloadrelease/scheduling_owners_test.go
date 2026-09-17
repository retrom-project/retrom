package payloadrelease

import (
	"errors"
	"math"
	"testing"

	model "retrom/internal/model/payloadrelease"
)

func TestTerminalSourceEligibilityAndSharedRelease(t *testing.T) {
	t.Parallel()
	for _, kind := range []model.ScopeType{model.ScopePegasusImportItem, model.ScopeEmulationStationImportItem} {
		for _, state := range []string{"REVIEW_PENDING", "COMMIT_FAILED", "REVIEW_DISCARDED"} {
			for _, retryable := range []bool{false, true} {
				t.Run(string(kind)+"/"+state+"/retryable="+boolName(retryable), func(t *testing.T) {
					t.Parallel()
					ref := model.Scope{Type: kind, ID: "source"}
					owner := model.Owner{Scope: ref, Version: 3, State: state, Retryable: retryable, PayloadState: "RETAINED"}
					records := &scheduleMemory{owners: map[model.Scope]model.Owner{ref: owner}}
					id, err := NewScheduler(scheduleIDs()).TerminalSource(t.Context(), records, ref, 10)
					eligible := state == "REVIEW_DISCARDED" || state == "COMMIT_FAILED" && !retryable
					if err != nil || (id != "") != eligible || (len(records.changes) == 1) != eligible {
						t.Fatalf("wrong terminal policy: %q/%v records=%+v", id, err, records)
					}
					if eligible && records.changes[0].Before != owner {
						t.Fatal("release lost owner compare-and-swap snapshot")
					}
				})
			}
		}
	}
}

func boolName(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestBoundSourceSharesOrdinaryReleaseWithoutNewJob(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"RELEASING", "RELEASED", "FAILED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ref := model.Scope{Type: model.ScopeEmulationStationImportItem, ID: "source"}
			ordinary := model.Scope{Type: model.ScopeImportItem, ID: "ordinary"}
			owner := model.Owner{Scope: ref, Version: 9, State: "PUBLISHED", PayloadState: "RETAINED", PublicID: ordinary.ID}
			records := &scheduleMemory{owners: map[model.Scope]model.Owner{
				ref: owner, ordinary: {Scope: ordinary, Version: 5, State: "PUBLISHED", PayloadState: state, ReleaseJobID: "shared"},
			}}
			id, err := NewScheduler(scheduleIDs()).TerminalSource(t.Context(), records, ref, 10)
			if err != nil || id != "shared" || len(records.jobs) != 0 || len(records.changes) != 1 ||
				records.changes[0] != (model.OwnerRelease{Before: owner, JobID: "shared", NowMS: 10}) {
				t.Fatalf("source duplicated release: %q/%v records=%+v", id, err, records)
			}
		})
	}
}

func TestSchedulerReplaysOwnerReleaseAndRejectsVersionOverflow(t *testing.T) {
	t.Parallel()
	ref := model.Scope{Type: model.ScopeImportItem, ID: "item"}
	for _, state := range []string{"RELEASING", "RELEASED", "FAILED", "RETAINED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			owner := model.Owner{Scope: ref, State: "PUBLISHED", Version: math.MaxInt64, PayloadState: state, ReleaseJobID: "existing"}
			records := &scheduleMemory{owners: map[model.Scope]model.Owner{ref: owner}}
			id, err := NewScheduler(scheduleIDs()).TerminalItem(t.Context(), records, ref.ID, model.ReasonImportPublished, 10)
			if state == "RETAINED" {
				if !errors.Is(err, model.ErrScopeInvalid) || id != "" {
					t.Fatalf("overflow accepted: %q/%v", id, err)
				}
			} else if err != nil || id != "existing" {
				t.Fatalf("lost replay: %q/%v", id, err)
			}
			if len(records.jobs) != 0 || len(records.changes) != 0 {
				t.Fatal("replay or invalid owner created writes")
			}
		})
	}
}

func TestImportReleaseWaitsForChildrenAndGameDeletionFencesVersion(t *testing.T) {
	t.Parallel()
	scheduler := NewScheduler(scheduleIDs())
	records := &scheduleMemory{pending: 1}
	id, err := scheduler.TerminalImport(t.Context(), records, "import", 10)
	if id != "" || err != nil || len(records.reads) != 0 || len(records.jobs) != 0 {
		t.Fatalf("live children scheduled: %q/%v %+v", id, err, records)
	}
	ref := model.Scope{Type: model.ScopeGame, ID: "game"}
	owner := model.Owner{Scope: ref, Version: 5, State: "PUBLISHED", PayloadState: "RETAINED"}
	records = &scheduleMemory{owners: map[model.Scope]model.Owner{ref: owner}}
	if id, err := scheduler.DeleteGame(t.Context(), records, ref.ID, 4, 10); id != "" || !errors.Is(err, model.ErrScopeInvalid) {
		t.Fatalf("stale game scheduled: %q/%v", id, err)
	}
	if len(records.jobs) != 0 {
		t.Fatal("stale game created release job")
	}
	id, err = scheduler.DeleteGame(t.Context(), records, ref.ID, 5, 10)
	if err != nil || id == "" || len(records.changes) != 1 || !records.changes[0].DeleteGame ||
		records.changes[0].Before != owner {
		t.Fatalf("game deletion lost snapshot: %q/%v %+v", id, err, records)
	}
}

func TestConsumptionReleaseRespectsExistingTerminalFacts(t *testing.T) {
	t.Parallel()
	for _, before := range []model.Consumption{
		{Version: 3, Released: true}, {Version: 3, ExistingJobID: "existing"}, {Version: 3},
	} {
		records := &scheduleMemory{consumption: before}
		id, err := NewScheduler(scheduleIDs()).Consumption(t.Context(), records, "consumption", 10)
		if err != nil {
			t.Fatal(err)
		}
		if before.Released && id != "" || before.ExistingJobID != "" && id != before.ExistingJobID {
			t.Fatalf("consumption replay changed: %+v id=%s", before, id)
		}
		if (len(records.jobs) == 1) != (!before.Released && before.ExistingJobID == "") || len(records.changes) != 0 {
			t.Fatalf("consumption repeated writes: %+v", records)
		}
	}
}
