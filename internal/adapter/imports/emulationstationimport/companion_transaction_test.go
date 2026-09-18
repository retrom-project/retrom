package emulationstationimport

import (
	"context"
	"errors"
	"testing"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
	persistence "retrom/internal/repo/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

func companionRecordFixture(
	t *testing.T,
) (
	lifecycleFixture,
	work,
	emulationstationimportmodel.CompanionOwner,
	emulationstationimportmodel.CompanionFile,
	emulationstationimportmodel.VerifiedBlob,
) {
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

func TestESCompanionLateFailureRollsBackCatalogAndResponse(t *testing.T) {
	for _, stage := range []string{"pre-commit hook", "commit"} {
		t.Run(stage, func(t *testing.T) {
			fixture, unit, owner, file, blob := companionRecordFixture(t)
			before := materialAuthoritySnapshot(t, fixture, owner.Before.Item.ID)
			ctx, cancel := context.WithCancel(fixture.context)
			defer cancel()
			cause := errors.New("late companion failure")
			repo := persistence.NewCompanions(fixture.database)
			if stage == "pre-commit hook" {
				repo.WithPreCommitHook(func(_ dbexec.Executor) error {
					return cause
				})
			} else {
				repo.WithPreCommitHook(func(_ dbexec.Executor) error {
					cancel()
					return nil
				})
				cause = context.Canceled
			}
			service := emulationstationimportservice.NewCompanions(
				repo,
				importExecutorAdapter{service: fixture.service},
				fixture.service.now,
			)
			id, err := service.Record(ctx, unit, owner, file, blob)
			if !errors.Is(err, cause) || id != "" {
				t.Fatalf("id=%s error=%v", id, err)
			}
			if before != materialAuthoritySnapshot(
				t, fixture, owner.Before.Item.ID,
			) {
				t.Fatal("failed transaction retained catalog")
			}
		})
	}
}
