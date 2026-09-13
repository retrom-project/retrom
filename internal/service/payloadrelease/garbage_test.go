package payloadrelease

import (
	"context"
	"errors"
	"testing"
)

type garbageFixture struct {
	facts                     GarbageFacts
	commitErr                 error
	removed, cancelled, files int
	checks                    int
	checkErr                  error
}

func (fixture *garbageFixture) WithGarbage(_ context.Context, run func(GarbageScope) error) error {
	err := run(GarbageScope{Read: fixture, Write: fixture})
	if err == nil {
		err = fixture.commitErr
	}
	if err != nil {
		fixture.removed, fixture.cancelled = 0, 0
	}
	return err
}

func (fixture *garbageFixture) Facts(context.Context, string, string) (GarbageFacts, error) {
	return fixture.facts, nil
}

func (fixture *garbageFixture) Remove(context.Context, GarbageFacts) error {
	fixture.removed++
	return nil
}

func (fixture *garbageFixture) Cancel(context.Context, GarbageFacts) error {
	fixture.cancelled++
	return nil
}

func (fixture *garbageFixture) Delete(context.Context, string) error { fixture.files++; return nil }
func (fixture *garbageFixture) CheckInScope(context.Context, WorkerScope, Work) error {
	fixture.checks++
	if fixture.checks == 2 {
		return fixture.checkErr
	}
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

func garbageTestExecution() Execution {
	return Execution{
		Work: Work{ID: "garbage-job", Scope: Scope{Type: ScopeBlob, ID: "garbage-blob"}},
		Input: Input{
			SchemaVersion: 1, Kind: "BLOB_GC", Scope: Scope{Type: ScopeBlob, ID: "garbage-blob"},
			Inputs: ScopeInputs{SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}
}

func TestGarbageDoesNotDeleteExistingBlobWithoutItsCandidate(t *testing.T) {
	t.Parallel()
	unit := garbageTestExecution()
	fixture := &garbageFixture{facts: GarbageFacts{Found: true, Blob: GCBlob{
		ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256,
	}}}
	err := NewGarbageCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
	if err != nil || fixture.files != 0 || fixture.removed != 0 {
		t.Fatalf("cancelled candidate allowed immediate deletion: %v files=%d catalog=%d", err, fixture.files, fixture.removed)
	}
}

func TestGarbageRejectsLostAuthorityBeforePublishingRemoval(t *testing.T) {
	t.Parallel()
	unit := garbageTestExecution()
	fixture := &garbageFixture{checkErr: ErrExecutionLost, facts: GarbageFacts{Found: true, Blob: GCBlob{
		ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256, HasCandidate: true,
		Candidate: GCCandidate{Work: unit.Work},
	}}}
	err := NewGarbageCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
	if !errors.Is(err, ErrExecutionLost) || fixture.files != 0 || fixture.removed != 0 || fixture.checks != 2 {
		t.Fatalf("late authority failure removed bytes: %v files=%d catalog=%d", err, fixture.files, fixture.removed)
	}
}

func TestGarbageProtectsCurrentReferencesAndReplacementCandidate(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"protected", "replacement"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			unit := garbageTestExecution()
			blob := GCBlob{
				ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256, HasCandidate: true,
				Candidate: GCCandidate{Work: unit.Work},
			}
			if scenario == "protected" {
				blob.Protected = true
			} else {
				blob.Candidate.Work.ID = "replacement-job"
			}
			fixture := &garbageFixture{facts: GarbageFacts{Found: true, Blob: blob}}
			err := NewGarbageCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
			if err != nil || fixture.files != 0 || fixture.removed != 0 || (fixture.cancelled == 1) != blob.Protected {
				t.Fatalf("garbage protection=%s error=%v files=%d catalog=%d cancelled=%d",
					scenario, err, fixture.files, fixture.removed, fixture.cancelled)
			}
		})
	}
}
