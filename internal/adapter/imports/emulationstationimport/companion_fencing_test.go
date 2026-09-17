package emulationstationimport

import (
	"errors"
	"testing"

	application "retrom/internal/model/emulationstationimport"
	persistence "retrom/internal/repo/emulationstationimport"
)

func TestESCompanionWriterRepeatsFrozenAuthority(t *testing.T) {
	for _, field := range []string{
		"worker",
		"job version",
		"attempt",
		"execution",
		"lease",
		"deadline",
		"item version",
		"collection",
		"mapping version",
		"target version",
		"target core",
		"candidate collection",
		"candidate facts",
		"candidate path",
		"candidate size",
	} {
		t.Run(field, func(t *testing.T) {
			fixture, _, original, file, blob := companionRecordFixture(t)
			before := materialAuthoritySnapshot(t, fixture, original.Before.Item.ID)
			err := persistence.NewCompanions(fixture.database).WithCompanions(
				fixture.context,
				func(scope application.CompanionScope) error {
					owner, err := scope.Read.Owner(fixture.context, original.Before.Item.ID)
					if err != nil {
						return err
					}
					change := application.CompanionBinding{Before: owner, File: file, Blob: blob, NowMS: fixture.now.UnixMilli()}
					mutateCompanionBinding(&change, field)
					_, err = scope.Write.Register(fixture.context, change)
					return err
				},
			)
			if !errors.Is(err, ErrVersionConflict) {
				t.Fatalf("field=%s error=%v", field, err)
			}
			if before != materialAuthoritySnapshot(t, fixture, original.Before.Item.ID) {
				t.Fatal("stale companion retained catalog or source")
			}
		})
	}
}

func mutateCompanionBinding(change *application.CompanionBinding, field string) {
	switch field {
	case "worker":
		change.Before.Before.Execution.WorkerID = "other"
	case "job version":
		change.Before.Before.Execution.JobVersion++
	case "attempt":
		change.Before.Before.Execution.Attempt++
	case "execution":
		change.Before.Before.Execution.ExecutionNo++
	case "lease":
		change.Before.Before.Execution.LeaseUntilMS++
	case "deadline":
		change.Before.Before.Execution.DeadlineAtMS++
	case "item version":
		change.Before.Before.Item.Version++
	case "collection":
		change.Before.CollectionID = "other"
	case "mapping version":
		change.Before.MappingVersion++
	case "target version":
		change.Before.Mapping.InstanceVersion++
	case "target core":
		change.Before.Mapping.CoreID = "other"
	default:
		mutateCompanionFile(&change.File, field)
	}
}

func mutateCompanionFile(file *application.CompanionFile, field string) {
	switch field {
	case "candidate collection":
		file.CollectionID = "other"
	case "candidate facts":
		file.Facts = "other"
	case "candidate path":
		file.Path = "other.zip"
	case "candidate size":
		file.Size++
	}
}
