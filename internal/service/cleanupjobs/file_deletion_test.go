package cleanupjobs

import (
	"context"
	"errors"
	"testing"
)

type fileDeletionFixture struct {
	facts                     FileDeletionFacts
	commitErr                 error
	removed, cancelled, files int
	checks                    int
	checkErr                  error
}

func (fixture *fileDeletionFixture) WithFileDeletion(_ context.Context, run func(FileDeletionScope) error) error {
	err := run(FileDeletionScope{Read: fixture, Write: fixture})
	if err == nil {
		err = fixture.commitErr
	}
	if err != nil {
		fixture.removed, fixture.cancelled = 0, 0
	}
	return err
}

func (fixture *fileDeletionFixture) Facts(context.Context, string, string) (FileDeletionFacts, error) {
	return fixture.facts, nil
}

func (fixture *fileDeletionFixture) Remove(context.Context, FileDeletionFacts) error {
	fixture.removed++
	return nil
}

func (fixture *fileDeletionFixture) Delete(context.Context, string) error {
	fixture.files++
	return nil
}

func (fixture *fileDeletionFixture) CheckInScope(context.Context, WorkerScope, Work) error {
	fixture.checks++
	if fixture.checks == 2 {
		return fixture.checkErr
	}
	return nil
}

func TestFileDeletionCommitFailureCannotRemovePhysicalFile(t *testing.T) {
	t.Parallel()
	cause := errors.New("file deletion commit unavailable")
	fixture := &fileDeletionFixture{commitErr: cause}
	unit := fileDeletionTestExecution()
	err := NewFileDeletionCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
	if !errors.Is(err, cause) || fixture.files != 0 {
		t.Fatalf("commit failure removed bytes: %v files=%d", err, fixture.files)
	}
}

func fileDeletionTestExecution() Execution {
	return Execution{
		Work: Work{ID: "file deletion-job", Scope: Scope{Type: ScopeFile, ID: "file deletion-blob"}},
		Input: Input{
			SchemaVersion: 1, Kind: "FILE_DELETE", Scope: Scope{Type: ScopeFile, ID: "file deletion-blob"},
			Inputs: ScopeInputs{SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}
}

func TestFileDeletionDoesNotDeleteExistingBlobWithoutItsCandidate(t *testing.T) {
	t.Parallel()
	unit := fileDeletionTestExecution()
	fixture := &fileDeletionFixture{facts: FileDeletionFacts{Found: true, Blob: DeletionFile{
		ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256,
	}}}
	err := NewFileDeletionCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
	if err == nil || fixture.files != 0 || fixture.removed != 0 {
		t.Fatalf("cancelled candidate allowed immediate deletion: %v files=%d catalog=%d", err, fixture.files, fixture.removed)
	}
}

func TestFileDeletionKeepsRetiredCatalogForRetryAfterLostAuthority(t *testing.T) {
	t.Parallel()
	unit := fileDeletionTestExecution()
	fixture := &fileDeletionFixture{checkErr: ErrExecutionLost, facts: FileDeletionFacts{Found: true, Blob: DeletionFile{
		ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256, HasCandidate: true,
		Candidate: DeletionCandidate{Work: unit.Work},
	}}}
	err := NewFileDeletionCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
	if !errors.Is(err, ErrExecutionLost) || fixture.files != 1 || fixture.removed != 0 || fixture.checks != 2 {
		t.Fatalf("late authority failure lost retry evidence: %v files=%d catalog=%d", err, fixture.files, fixture.removed)
	}
}

func TestFileDeletionProtectsCurrentReferencesAndReplacementCandidate(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"protected", "replacement"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			unit := fileDeletionTestExecution()
			blob := DeletionFile{
				ID: unit.Work.Scope.ID, Digest: unit.Input.Inputs.SHA256, HasCandidate: true,
				Candidate: DeletionCandidate{Work: unit.Work},
			}
			if scenario == "protected" {
				blob.Retained = true
			} else {
				blob.Candidate.Work.ID = "replacement-job"
			}
			fixture := &fileDeletionFixture{facts: FileDeletionFacts{Found: true, Blob: blob}}
			err := NewFileDeletionCollector(fixture, fixture, fixture).Execute(t.Context(), unit)
			if err == nil || fixture.files != 0 || fixture.removed != 0 {
				t.Fatalf("file deletion protection=%s error=%v files=%d catalog=%d cancelled=%d",
					scenario, err, fixture.files, fixture.removed, fixture.cancelled)
			}
		})
	}
}
