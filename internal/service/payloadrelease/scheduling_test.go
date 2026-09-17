package payloadrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	model "retrom/internal/model/payloadrelease"
	"testing"
)

type scheduleMemory struct {
	owners                       map[model.Scope]model.Owner
	pending                      int64
	consumption                  model.Consumption
	readErr, createErr, beginErr error
	jobs                         []model.ScheduledJob
	changes                      []model.OwnerRelease
	reads                        []model.Scope
}

func (records *scheduleMemory) Owner(_ context.Context, ref model.Scope) (model.Owner, error) {
	records.reads = append(records.reads, ref)
	return records.owners[ref], records.readErr
}

func (records *scheduleMemory) PendingChildren(context.Context, string) (int64, error) {
	return records.pending, records.readErr
}

func (records *scheduleMemory) Consumption(context.Context, string) (model.Consumption, error) {
	return records.consumption, records.readErr
}

func (records *scheduleMemory) CreateJob(_ context.Context, job model.ScheduledJob) error {
	records.jobs = append(records.jobs, job)
	return records.createErr
}

func (records *scheduleMemory) BeginRelease(_ context.Context, change model.OwnerRelease) error {
	records.changes = append(records.changes, change)
	return records.beginErr
}

func scheduleRequest() model.ScheduleRequest {
	return model.ScheduleRequest{
		Scope: model.Scope{Type: model.ScopeImportItem, ID: "item"}, ScopeVersion: 2,
		Reason: model.ReasonImportPublished, NowMS: 10,
	}
}

func scheduleIDs() func() (string, error) {
	var n int
	return func() (string, error) { n++; return fmt.Sprintf("identity-%d", n), nil }
}

func TestSchedulerFreezesCheckedIdentitiesAndCanonicalInput(t *testing.T) {
	t.Parallel()
	records := &scheduleMemory{}
	request := scheduleRequest()
	id, err := NewScheduler(scheduleIDs()).Queue(t.Context(), records, request)
	if err != nil || id != "identity-1" || len(records.jobs) != 1 {
		t.Fatalf("schedule=%q/%v jobs=%+v", id, err, records.jobs)
	}
	job := records.jobs[0]
	var input model.Input
	if err := json.Unmarshal([]byte(job.InputJSON), &input); err != nil {
		t.Fatal(err)
	}
	expected := model.Input{
		SchemaVersion: 1, Kind: "PAYLOAD_RELEASE", Scope: request.Scope, ExecutionID: "identity-2",
		Inputs: model.ScopeInputs{ScopeVersion: 2, Reason: model.ReasonImportPublished},
	}
	digest := sha256.Sum256([]byte(job.InputJSON))
	dedupe := sha256.Sum256([]byte("retrom-job-dedupe-v1\x00PAYLOAD_RELEASE\x00IMPORT_ITEM\x00item"))
	if !reflect.DeepEqual(input, expected) || job.InputDigest != hex.EncodeToString(digest[:]) ||
		job.DedupeKey != hex.EncodeToString(dedupe[:]) || job.Scope != request.Scope || job.NowMS != 10 {
		t.Fatalf("wrong durable input: %+v %+v", job, input)
	}
}

func TestSchedulerIdentityFailuresCannotWrite(t *testing.T) {
	t.Parallel()
	cause := errors.New("identity source unavailable")
	for _, failAt := range []int{1, 2} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/empty=%t", failAt, empty), func(t *testing.T) {
				t.Parallel()
				calls := 0
				scheduler := NewScheduler(func() (string, error) {
					calls++
					if calls == failAt {
						if empty {
							return "", nil
						}
						return "", cause
					}
					return "job", nil
				})
				records := &scheduleMemory{}
				id, err := scheduler.Queue(t.Context(), records, scheduleRequest())
				if id != "" || !errors.Is(err, model.ErrScheduleIDInvalid) || (!empty && !errors.Is(err, cause)) ||
					calls != failAt || len(records.jobs) != 0 || len(records.changes) != 0 {
					t.Fatalf("identity failure wrote or lost cause: %q/%v calls=%d records=%+v", id, err, calls, records)
				}
			})
		}
	}
}

func TestSchedulerRejectsInvalidRequestsBeforeIdentityOrStorage(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*model.ScheduleRequest){
		"blob":    func(value *model.ScheduleRequest) { value.Scope.Type = model.ScopeBlob },
		"unknown": func(value *model.ScheduleRequest) { value.Scope.Type = "UNKNOWN" },
		"empty":   func(value *model.ScheduleRequest) { value.Scope.ID = "" },
		"version": func(value *model.ScheduleRequest) { value.ScopeVersion = 0 },
		"reason":  func(value *model.ScheduleRequest) { value.Reason = "UNKNOWN" },
		"clock":   func(value *model.ScheduleRequest) { value.NowMS = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := scheduleRequest()
			change(&request)
			called := false
			scheduler := NewScheduler(func() (string, error) { called = true; return "id", nil })
			records := &scheduleMemory{}
			id, err := scheduler.Queue(t.Context(), records, request)
			if id != "" || !errors.Is(err, model.ErrScopeInvalid) || called || len(records.jobs) != 0 {
				t.Fatalf("invalid request entered storage: %q/%v called=%t records=%+v", id, err, called, records)
			}
		})
	}
}

func TestSchedulerDoesNotExposeIdentityAfterStorageFailure(t *testing.T) {
	t.Parallel()
	cause := errors.New("transaction write failed")
	ref := model.Scope{Type: model.ScopeImportItem, ID: "item"}
	owner := model.Owner{Scope: ref, State: "PUBLISHED", PayloadState: "RETAINED", Version: 7}
	for _, phase := range []string{"read", "job", "owner"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			records := &scheduleMemory{owners: map[model.Scope]model.Owner{ref: owner}}
			switch phase {
			case "read":
				records.readErr = cause
			case "job":
				records.createErr = cause
			case "owner":
				records.beginErr = cause
			}
			id, err := NewScheduler(scheduleIDs()).TerminalItem(t.Context(), records, ref.ID, model.ReasonImportPublished, 10)
			if id != "" || !errors.Is(err, cause) {
				t.Fatalf("failed transaction exposed identity or lost cause: %q/%v", id, err)
			}
			if phase == "read" && len(records.jobs) != 0 || phase != "owner" && len(records.changes) != 0 {
				t.Fatalf("work continued after failure: %+v", records)
			}
		})
	}
}
