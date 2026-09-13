package libraryimport

import (
	"context"
	"errors"
	"testing"
	"time"
)

type sourceRecordsStub struct {
	snapshot          SourceCreationSnapshot
	readErr, writeErr error
	changes           []SourceBindingChange
}

func (records *sourceRecordsStub) ReadSource(context.Context, SourceCreationIntent) (SourceCreationSnapshot, error) {
	return records.snapshot, records.readErr
}

func (records *sourceRecordsStub) BindSource(_ context.Context, change SourceBindingChange) error {
	records.changes = append(records.changes, change)
	return records.writeErr
}

func sourceOwnershipFixture() (SourceCreationIntent, SourceCreationSnapshot) {
	intent := SourceCreationIntent{Kind: SourceOwnerPegasus, ImportID: "plan", ItemID: "source", JobID: "work", WorkerID: "worker", ExecutionNo: 1, Attempt: 1, PrimaryPaths: []string{"games/main.gba"}}
	snapshot := SourceCreationSnapshot{Kind: SourceOwnerPegasus, ImportID: "plan", ItemID: "source", JobID: "work", WorkerID: "worker", ExecutionNo: 1, Attempt: 1, SourceVersion: 2, ImportVersion: 3, JobVersion: 4, SourceState: "COPYING", ImportState: "RUNNING", JobState: "RUNNING", LeaseUntilMS: 100, DeadlineMS: 200, TargetPlatformInstanceID: "target", TargetVersion: 1, MappingAction: "IMPORT", PrimaryPaths: []string{"games/main.gba"}}
	return intent, snapshot
}

func TestSourceOwnershipRejectsStaleExecutionBeforeCreation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"worker", "execution", "attempt", "lease", "deadline", "cancel", "source", "target"} {
		t.Run(kind, func(t *testing.T) {
			intent, snapshot := sourceOwnershipFixture()
			switch kind {
			case "worker":
				snapshot.WorkerID = "other"
			case "execution":
				snapshot.ExecutionNo++
			case "attempt":
				snapshot.Attempt++
			case "lease":
				snapshot.LeaseUntilMS = 10
			case "deadline":
				snapshot.DeadlineMS = 10
			case "cancel":
				snapshot.JobState = "CANCEL_REQUESTED"
			case "source":
				snapshot.PrimaryPaths = []string{"other.gba"}
			case "target":
				snapshot.TargetPlatformInstanceID = "other"
			}
			service := NewSourceOwnership(func() time.Time { return time.UnixMilli(10) })
			result, err := service.Prepare(t.Context(), &sourceRecordsStub{snapshot: snapshot}, intent, "target")
			if !errors.Is(err, ErrVersionConflict) || result.ItemID != "" {
				t.Fatalf("stale %s accepted: %#v %v", kind, result, err)
			}
		})
	}
}

func TestSourceOwnershipRevalidatesSourceVersionAfterPreparation(t *testing.T) {
	t.Parallel()
	intent, before := sourceOwnershipFixture()
	after := before
	after.SourceVersion++
	service := NewSourceOwnership(func() time.Time { return time.UnixMilli(10) })
	result, err := service.Revalidate(t.Context(), &sourceRecordsStub{snapshot: after}, intent, before, "target")
	if !errors.Is(err, ErrVersionConflict) || result.ItemID != "" {
		t.Fatalf("changed source accepted: %#v %v", result, err)
	}
}

func TestSourceOwnershipPreservesStorageErrors(t *testing.T) {
	t.Parallel()
	intent, before := sourceOwnershipFixture()
	failure := errors.New("source read failed")
	service := NewSourceOwnership(func() time.Time { return time.UnixMilli(10) })
	result, err := service.Prepare(t.Context(), &sourceRecordsStub{readErr: failure}, intent, "target")
	if !errors.Is(err, failure) || result.ItemID != "" {
		t.Fatalf("read failure lost: %#v %v", result, err)
	}
	records := &sourceRecordsStub{writeErr: failure}
	err = service.Attach(t.Context(), records, before, ServerCreated{ImportJobID: "library"}, ServerImportItem{ItemID: "item", ContentKind: "SINGLE_FILE", SourceManifestJSON: "{}", SourceManifestDigest: "digest"})
	if !errors.Is(err, failure) {
		t.Fatalf("bind failure lost: %v", err)
	}
}

func TestOwnedSourceRequiresOneDeclaredPrimaryGroup(t *testing.T) {
	t.Parallel()
	wanted := []string{"games/main.zip"}
	for _, paths := range [][][]string{nil, {{"parent.zip"}}, {{"games/main.zip"}, {"parent.zip"}}, {{"games/main.zip"}, {"games/main.zip"}}} {
		if err := ValidateOwnedSourceGroups(wanted, paths); !errors.Is(err, ErrInvalid) || !errors.Is(err, ErrSourceGrouping) {
			t.Fatalf("unowned groups accepted: %#v %v", paths, err)
		}
	}
	if err := ValidateOwnedSourceGroups(nil, [][]string{{"games/main.zip"}}); !errors.Is(err, ErrInvalid) || !errors.Is(err, ErrSourceGrouping) {
		t.Fatalf("missing primary paths lack grouping classification: %v", err)
	}
	if err := ValidateOwnedSourceGroups(wanted, [][]string{{"games/main.zip"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSourceOwnershipAcceptsLeaseRenewalForSameExecution(t *testing.T) {
	t.Parallel()
	intent, before := sourceOwnershipFixture()
	renewed := before
	renewed.JobVersion++
	renewed.LeaseUntilMS++
	service := NewSourceOwnership(func() time.Time { return time.UnixMilli(10) })
	result, err := service.Revalidate(t.Context(), &sourceRecordsStub{snapshot: renewed}, intent, before, "target")
	if err != nil || result.JobVersion != renewed.JobVersion {
		t.Fatalf("legitimate heartbeat rejected: %#v %v", result, err)
	}
}

func TestOwnedSourceFilesMatchCopiedPrimaryButAllowDependencies(t *testing.T) {
	t.Parallel()
	source := ServerSourceFile{RelativePath: "main.zip", BlobID: "primary", SizeBytes: 1}
	snapshot := SourceCreationSnapshot{Files: []SourceCreationFile{{File: source, State: "COPIED"}}}
	inputs := []ServerSourceFile{source, {RelativePath: "parent.zip", BlobID: "parent", SizeBytes: 2}}
	if err := ValidateOwnedSourceFiles(snapshot, inputs); err != nil {
		t.Fatal(err)
	}
	inputs[0].BlobID = "other"
	if err := ValidateOwnedSourceFiles(snapshot, inputs); !errors.Is(err, ErrVersionConflict) || errors.Is(err, ErrSourceGrouping) {
		t.Fatalf("different copied blob accepted: %v", err)
	}
	inputs[0] = source
	snapshot.Files[0].State = "SOURCE_CHANGED"
	if err := ValidateOwnedSourceFiles(snapshot, inputs); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("changed source accepted: %v", err)
	}
}
