package launch

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	model "retrom/internal/model/launch"
	"testing"
	"time"
)

const validationFixtureID = "01980000-0000-7000-8000-000000000081"

func validationSchedulerFixture(repository *validationJobMemory) *ValidationScheduler {
	return NewValidationScheduler(repository, model.ValidationEnvironment{Now: func() time.Time { return time.UnixMilli(100) }, NewID: func() (string, error) { return validationFixtureID, nil }})
}

func validationRetrySnapshot(t *testing.T) model.ValidationSnapshot {
	t.Helper()
	return model.ValidationSnapshot{SchemaVersion: 1, Kind: "VARIANT_VALIDATE", Scope: model.ValidationScope{Type: "GAME_VARIANT", ID: "variant"}, ExecutionID: "previous", Inputs: model.ValidationInputs{GameID: "old-game", GameVariantID: "variant", GameVersion: 7, SourceManifestDigest: "frozen-content", ValidationInputDigest: "frozen-digest"}}
}

func TestValidationSchedulerRetainsRetryInputs(t *testing.T) {
	snapshot := validationRetrySnapshot(t)
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	repository := &validationJobMemory{found: true, current: model.ValidationJob{ID: validationFixtureID, State: "FAILED", Retryable: true, Version: 5, ExecutionNo: 3, SnapshotJSON: string(encoded)}}
	result, err := validationSchedulerFixture(repository).Queue(t.Context(), model.ValidationInputs{GameID: "replacement-game", GameVariantID: "variant", ValidationInputDigest: "new-digest"})
	if err != nil || !result.Queued || result.JobID != validationFixtureID || len(repository.writes) != 1 {
		t.Fatalf("result=%+v error=%v writes=%d", result, err, len(repository.writes))
	}
	plan := repository.writes[0]
	var actual model.ValidationSnapshot
	if err := json.Unmarshal([]byte(plan.SnapshotJSON), &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.Inputs, actual.Inputs) || actual.ExecutionID != validationFixtureID || plan.ExecutionNo != 4 || plan.PreviousVersion != 5 || !plan.Retry {
		t.Fatalf("retry=%+v snapshot=%+v", plan, actual)
	}
}

func TestValidationSchedulerReusesCurrentAndRejectsTerminalJobs(t *testing.T) {
	for _, test := range []struct {
		state              string
		retryable, blocked bool
	}{
		{"QUEUED", false, false},
		{"RUNNING", false, false},
		{"SUCCEEDED", false, false},
		{"FAILED", false, true},
		{"CANCELLED", false, true},
		{"CANCELLED", true, true},
	} {
		t.Run(test.state+map[bool]string{true: "-retryable", false: ""}[test.retryable], func(t *testing.T) {
			repository := &validationJobMemory{found: true, current: model.ValidationJob{ID: validationFixtureID, State: test.state, Retryable: test.retryable}}
			result, err := validationSchedulerFixture(repository).Queue(t.Context(), model.ValidationInputs{GameVariantID: "variant"})
			if errors.Is(err, model.ErrBlocked) != test.blocked || result.Queued || len(repository.writes) != 0 {
				t.Fatalf("result=%+v error=%v writes=%d", result, err, len(repository.writes))
			}
			if !test.blocked && result.JobID != validationFixtureID {
				t.Fatal("replay lost job identity")
			}
		})
	}
}

func TestValidationSchedulerRejectsInvalidRetriesAndIdentities(t *testing.T) {
	snapshot := validationRetrySnapshot(t)
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, body         string
		execution, version int64
	}{
		{"malformed", "broken", 1, 1},
		{"wrong scope", `{"schemaVersion":1,"kind":"VARIANT_VALIDATE","scope":{"type":"GAME","id":"variant"}}`, 1, 1},
		{"exhausted execution", string(encoded), math.MaxInt64, 1},
		{"exhausted version", string(encoded), 1, math.MaxInt64},
		{"zero execution", string(encoded), 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &validationJobMemory{found: true, current: model.ValidationJob{ID: validationFixtureID, State: "FAILED", Retryable: true, SnapshotJSON: test.body, ExecutionNo: test.execution, Version: test.version}}
			result, err := validationSchedulerFixture(repository).Queue(t.Context(), model.ValidationInputs{GameVariantID: "variant"})
			if err == nil || result.JobID != "" || len(repository.writes) != 0 {
				t.Fatalf("result=%+v error=%v writes=%d", result, err, len(repository.writes))
			}
		})
	}
	for _, id := range []string{"", "00000000-0000-0000-0000-000000000000", "01980000000070008000000000000081"} {
		t.Run("identity-"+id, func(t *testing.T) {
			repository := &validationJobMemory{}
			scheduler := validationSchedulerFixture(repository)
			scheduler.environment.NewID = func() (string, error) { return id, nil }
			_, err := scheduler.Queue(t.Context(), model.ValidationInputs{GameVariantID: "variant"})
			if !errors.Is(err, model.ErrBlocked) || len(repository.writes) != 0 {
				t.Fatalf("error=%v writes=%d", err, len(repository.writes))
			}
		})
	}
}
