package cleanupjobs

import (
	"errors"
	"math"
	"testing"

	jobs "retrom/internal/service/cleanupjobs"
	gamecleanup "retrom/internal/service/gamecontent/payloadpolicy"
	importcleanup "retrom/internal/service/libraryimport/payloadpolicy"
	sourcecleanup "retrom/internal/service/sourceimport/payloadpolicy"
	uploadcleanup "retrom/internal/service/uploads/payloadpolicy"
)

func TestTerminalSourceEligibility(t *testing.T) {
	t.Parallel()
	for _, kind := range []jobs.ScopeType{jobs.ScopeSourceImportItem, jobs.ScopeSourceImportItem} {
		for _, state := range []string{"REVIEW_PENDING", "COMMIT_FAILED", "REVIEW_DISCARDED"} {
			for _, retryable := range []bool{false, true} {
				t.Run(string(kind)+"/"+state+"/retryable="+boolName(retryable), func(t *testing.T) {
					t.Parallel()
					ref := jobs.Scope{Type: kind, ID: "source"}
					owner := jobs.Owner{Scope: ref, Version: 3, State: state, Retryable: retryable, PayloadState: "RETAINED"}
					records := &scheduleMemory{owners: map[jobs.Scope]jobs.Owner{ref: owner}}
					id, err := sourcecleanup.TerminalSource(t.Context(), jobs.NewScheduler(scheduleIDs()), records, ref, 10)
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

func TestBoundSourceQueuesIndependentJobAtHandoff(t *testing.T) {
	ref := jobs.Scope{Type: jobs.ScopeSourceImportItem, ID: "source"}
	owner := jobs.Owner{Scope: ref, Version: 9, State: "REVIEW_PENDING", PayloadState: "RETAINED", PublicID: "ordinary"}
	records := &scheduleMemory{owners: map[jobs.Scope]jobs.Owner{ref: owner}}
	id, err := sourcecleanup.TerminalSource(t.Context(), jobs.NewScheduler(scheduleIDs()), records, ref, 10)
	if err != nil || id == "" || len(records.jobs) != 1 || len(records.changes) != 1 || len(records.reads) != 1 || records.reads[0] != ref {
		t.Fatalf("handoff release consulted another owner: %q/%v records=%+v", id, err, records)
	}
	if records.jobs[0].Scope != ref || records.changes[0] != (jobs.OwnerRelease{Before: owner, JobID: id, NowMS: 10}) {
		t.Fatal("lost independent release identity")
	}
}

func TestSchedulerReplaysOwnerReleaseAndRejectsVersionOverflow(t *testing.T) {
	t.Parallel()
	ref := jobs.Scope{Type: jobs.ScopeImportItem, ID: "item"}
	for _, state := range []string{"RELEASING", "RELEASED", "RETAINED"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			owner := jobs.Owner{Scope: ref, State: "PUBLISHED", Version: math.MaxInt64, PayloadState: state, ReleaseJobID: "existing"}
			records := &scheduleMemory{owners: map[jobs.Scope]jobs.Owner{ref: owner}}
			id, err := importcleanup.TerminalItem(t.Context(), jobs.NewScheduler(scheduleIDs()), records, ref.ID, jobs.ReasonImportPublished, 10)
			if state == "RETAINED" {
				if !errors.Is(err, jobs.ErrScopeInvalid) || id != "" {
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
	scheduler := jobs.NewScheduler(scheduleIDs())
	records := &scheduleMemory{pending: 1}
	id, err := importcleanup.TerminalImport(t.Context(), scheduler, records, "import", 10)
	if id != "" || err != nil || len(records.reads) != 0 || len(records.jobs) != 0 {
		t.Fatalf("live children scheduled: %q/%v %+v", id, err, records)
	}
	ref := jobs.Scope{Type: jobs.ScopeGame, ID: "game"}
	owner := jobs.Owner{Scope: ref, Version: 5, State: "PUBLISHED", PayloadState: "RETAINED"}
	records = &scheduleMemory{owners: map[jobs.Scope]jobs.Owner{ref: owner}}
	if id, err := gamecleanup.DeleteGame(t.Context(), scheduler, records, ref.ID, 4, 10); id != "" || !errors.Is(err, jobs.ErrScopeInvalid) {
		t.Fatalf("stale game scheduled: %q/%v", id, err)
	}
	if len(records.jobs) != 0 {
		t.Fatal("stale game created release job")
	}
	id, err = gamecleanup.DeleteGame(t.Context(), scheduler, records, ref.ID, 5, 10)
	if err != nil || id == "" || len(records.changes) != 1 || !records.changes[0].DeleteGame ||
		records.changes[0].Before != owner {
		t.Fatalf("game deletion lost snapshot: %q/%v %+v", id, err, records)
	}
}

func TestConsumptionReleaseRespectsExistingTerminalFacts(t *testing.T) {
	t.Parallel()
	for _, before := range []jobs.Consumption{
		{Version: 3, Released: true}, {Version: 3, ExistingJobID: "existing"}, {Version: 3},
	} {
		records := &scheduleMemory{consumption: before}
		id, err := uploadcleanup.Consumption(t.Context(), jobs.NewScheduler(scheduleIDs()), records, "consumption", 10)
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
