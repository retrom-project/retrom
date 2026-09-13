package emulationstationimport

import (
	"context"
	"errors"
	"testing"

	persistence "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func companionRecordFixture(
	t *testing.T,
) (lifecycleFixture, work, application.CompanionOwner, application.CompanionFile, application.VerifiedBlob) {
	t.Helper()
	fixture, unit, item := companionFixture(t)
	selection, err := fixture.service.companions().Find(fixture.context, unit, item.ID)
	if err != nil || len(selection.Files) != 2 {
		t.Fatalf("files=%#v error=%v", selection.Files, err)
	}
	file := selection.Files[0]
	blob, err := (importExecutorAdapter{service: fixture.service}).CopyFile(
		fixture.context,
		unit,
		executionFile{Path: file.Path, Size: file.Size, Facts: file.Facts},
	)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, unit, selection.Owner, file, blob
}

func TestESCompanionRegistrationReusesOnlyMatchingVerifiedCatalog(t *testing.T) {
	fixture, unit, owner, file, blob := companionRecordFixture(t)
	service := fixture.service.companions()
	id, err := service.Record(fixture.context, unit, owner, file, blob)
	if err != nil || id == "" {
		t.Fatalf("id=%s error=%v", id, err)
	}
	repeated, err := service.Record(fixture.context, unit, owner, file, blob)
	if err != nil || repeated != id {
		t.Fatalf("replay=%s error=%v", repeated, err)
	}
	before := materialAuthoritySnapshot(t, fixture, owner.Before.Item.ID)
	blob.MD5 = "00000000000000000000000000000000"
	result, err := service.Record(fixture.context, unit, owner, file, blob)
	if !errors.Is(err, ErrVersionConflict) || result != "" {
		t.Fatalf("collision result=%s error=%v", result, err)
	}
	if before != materialAuthoritySnapshot(t, fixture, owner.Before.Item.ID) {
		t.Fatal("collision changed catalog or source")
	}
}

type lateCompanionRepository struct {
	application.CompanionRepository
	stop  context.CancelFunc
	cause error
}

func (repository lateCompanionRepository) WithCompanions(
	ctx context.Context,
	run func(application.CompanionScope) error,
) error {
	return repository.CompanionRepository.WithCompanions(ctx, func(scope application.CompanionScope) error {
		if err := run(scope); err != nil {
			return err
		}
		if repository.stop != nil {
			repository.stop()
		}
		return repository.cause
	})
}

func TestESCompanionLateFailureRollsBackCatalogAndResponse(t *testing.T) {
	for _, stage := range []string{"callback", "commit"} {
		t.Run(stage, func(t *testing.T) {
			fixture, unit, owner, file, blob := companionRecordFixture(t)
			before := materialAuthoritySnapshot(t, fixture, owner.Before.Item.ID)
			ctx, cancel := context.WithCancel(fixture.context)
			defer cancel()
			cause := errors.New("late companion failure")
			repository := lateCompanionRepository{CompanionRepository: persistence.NewCompanions(fixture.database), cause: cause}
			if stage == "commit" {
				repository.cause = nil
				repository.stop = cancel
				cause = context.Canceled
			}
			service := application.NewCompanions(
				repository,
				importExecutorAdapter{service: fixture.service},
				fixture.service.now,
			)
			id, err := service.Record(ctx, unit, owner, file, blob)
			if !errors.Is(err, cause) || id != "" {
				t.Fatalf("id=%s error=%v", id, err)
			}
			if before != materialAuthoritySnapshot(t, fixture, owner.Before.Item.ID) {
				t.Fatal("failed transaction retained catalog")
			}
		})
	}
}
