package payloadrelease

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/payloadrelease"
)

type garbageFixture struct {
	facts                     model.GarbageFacts
	commitErr                 error
	removed, cancelled, files int
}

func (fixture *garbageFixture) LoadGarbageFacts(
	_ context.Context, _, _ string,
) (model.GarbageFacts, error) {
	return fixture.facts, nil
}

func (fixture *garbageFixture) LoadGarbageWork(
	_ context.Context, _ string,
) (model.Work, bool, error) {
	return model.Work{State: "RUNNING", WorkerID: "w", ExecutionNo: 1}, true, nil
}

func (fixture *garbageFixture) CommitGarbage(
	_ context.Context,
	cmd model.GarbageCommand,
	_ model.EffectAuthority,
) error {
	if fixture.commitErr != nil {
		return fixture.commitErr
	}
	if cmd.Remove {
		fixture.removed++
	}
	if cmd.Cancel {
		fixture.cancelled++
	}
	return nil
}

func (fixture *garbageFixture) Delete(context.Context, string) error {
	fixture.files++
	return nil
}

func (fixture *garbageFixture) CheckInScope(
	context.Context, model.WorkerScope, model.Work,
) error {
	return nil
}

func TestGarbageCommitFailureCannotRemovePhysicalFile(t *testing.T) {
	t.Parallel()
	cause := errors.New("garbage commit unavailable")
	fixture := &garbageFixture{commitErr: cause}
	unit := garbageTestExecution()
	err := NewGarbageCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
	if !errors.Is(err, cause) || fixture.files != 0 {
		t.Fatalf("commit failure removed bytes: %v files=%d", err, fixture.files)
	}
}

func garbageTestExecution() model.Execution {
	return model.Execution{
		Work: model.Work{
			ID: "garbage-job", State: "RUNNING", WorkerID: "w",
			ExecutionNo: 1,
			Scope:       model.Scope{Type: model.ScopeBlob, ID: "garbage-blob"},
		},
		Input: model.Input{
			SchemaVersion: 1, Kind: "BLOB_GC",
			Scope:  model.Scope{Type: model.ScopeBlob, ID: "garbage-blob"},
			Inputs: model.ScopeInputs{SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}
}

func TestGarbageDoesNotDeleteExistingBlobWithoutItsCandidate(t *testing.T) {
	t.Parallel()
	unit := garbageTestExecution()
	fixture := &garbageFixture{facts: model.GarbageFacts{Found: true, Blob: model.GCBlob{
		ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256,
	}}}
	err := NewGarbageCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
	if err != nil || fixture.files != 0 || fixture.removed != 0 {
		t.Fatalf("cancelled candidate allowed immediate deletion: %v files=%d catalog=%d", err, fixture.files, fixture.removed)
	}
}

type garbageLostAuthority struct{ garbageFixture }

func (fixture *garbageLostAuthority) LoadGarbageWork(
	_ context.Context, _ string,
) (model.Work, bool, error) {
	return model.Work{}, false, nil
}

func TestGarbageRejectsLostAuthorityBeforePublishingRemoval(t *testing.T) {
	t.Parallel()
	unit := garbageTestExecution()
	inner := &garbageFixture{facts: model.GarbageFacts{Found: true, Blob: model.GCBlob{
		ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256, HasCandidate: true,
		Candidate: model.GCCandidate{Work: unit.Work},
	}}}
	fixture := &garbageLostAuthority{garbageFixture: *inner}
	err := NewGarbageCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
	if !errors.Is(err, model.ErrExecutionLost) || fixture.files != 0 || fixture.removed != 0 {
		t.Fatalf("lost authority allowed removal: %v files=%d catalog=%d", err, fixture.files, fixture.removed)
	}
}

func TestGarbageProtectsCurrentReferencesAndReplacementCandidate(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"protected", "replacement"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			unit := garbageTestExecution()
			blob := model.GCBlob{
				ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256, HasCandidate: true,
				Candidate: model.GCCandidate{Work: unit.Work},
			}
			if scenario == "protected" {
				blob.Protected = true
			} else {
				blob.Candidate.Work.ID = "replacement-job"
			}
			fixture := &garbageFixture{facts: model.GarbageFacts{Found: true, Blob: blob}}
			err := NewGarbageCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
			if err != nil || fixture.files != 0 || fixture.removed != 0 || (fixture.cancelled == 1) != blob.Protected {
				t.Fatalf("garbage protection=%s error=%v files=%d catalog=%d cancelled=%d",
					scenario, err, fixture.files, fixture.removed, fixture.cancelled)
			}
		})
	}
}
