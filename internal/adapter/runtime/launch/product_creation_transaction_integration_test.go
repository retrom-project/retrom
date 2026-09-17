//go:build integration

package launch

import (
	"context"
	"errors"
	"testing"
	"time"

	application "retrom/internal/model/launch"
	persistence "retrom/internal/repo/launch"
)

type productFrozenSnapshotRepository struct {
	application.ProductCreationRepository
	snapshot application.ProductSnapshot
}

func (repository productFrozenSnapshotRepository) Snapshot(context.Context, application.ProductCreateCommand) (application.ProductSnapshot, error) {
	return repository.snapshot, nil
}

func assertProductSnapshotRejected(t *testing.T, service *Service, command application.ProductCreateCommand, snapshot application.ProductSnapshot) {
	t.Helper()
	fixture := reviewCheckpointFixture{database: service.database}
	before := playRows(t, service.database)
	repository := productFrozenSnapshotRepository{ProductCreationRepository: persistence.NewProductCreation(service.database), snapshot: snapshot}
	result, err := service.productCreator(repository).Create(t.Context(), command)
	if !errors.Is(err, ErrBlocked) || result.Created.LaunchID != "" {
		t.Fatalf("stale product snapshot: launch=%q error=%v", result.Created.LaunchID, err)
	}
	configDraftUnchanged(t, fixture, before)
}

func TestProductCreatorReadyValidationReentersThePublicEntry(t *testing.T) {
	fixture := newScummVMFixture(t, []string{"One"})
	approved, err := fixture.importer.Approve(t.Context(), fixture.itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	mustRPGLaunchSQL(t, fixture.database, `UPDATE game_variants SET status='BLOCKED',compatibility_code='VALIDATION_PENDING' WHERE game_id=?`, approved.GameID)
	original := fixture.service.now
	nested := *fixture.service
	nested.now = original
	invoked := false
	var jobID string
	fixture.service.now = func() time.Time {
		if !invoked {
			invoked = true
			pending, err := nested.EnsureVariantForMove(t.Context(), approved.GameID, "scummvm")
			if err != nil || pending.JobID == "" {
				t.Fatalf("nested validation job=%q error=%v", pending.JobID, err)
			}
			jobID = pending.JobID
			nested.ResumeValidationJob(t.Context(), jobID)
		}
		return original()
	}
	result, err := fixture.service.Create(t.Context(), "scummvm-profile", CreateRequest{GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: Capabilities{SecureContext: true}})
	if err != nil || !invoked || result.LaunchID == "" || result.Status == "VALIDATION_PENDING" {
		t.Fatalf("public READY reentry hook=%v launch=%q status=%s error=%v", invoked, result.LaunchID, result.Status, err)
	}
	var launches, jobs int
	var state string
	err = fixture.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM launch_sessions WHERE game_id=?),(SELECT count(*) FROM jobs WHERE kind='VARIANT_VALIDATE'),(SELECT state FROM jobs WHERE id=?)`, approved.GameID, jobID).Scan(&launches, &jobs, &state)
	if err != nil || launches != 1 || jobs != 1 || state != "SUCCEEDED" {
		t.Fatalf("reentry launches=%d jobs=%d state=%s error=%v", launches, jobs, state, err)
	}
}

func TestProductCreationAllowsMetadataEditBetweenSnapshots(t *testing.T) {
	fixture, request := productCreationFixture(t)
	original := fixture.launcher.now
	invoked := false
	fixture.launcher.now = func() time.Time {
		if !invoked {
			invoked = true
			mustRPGLaunchSQL(t, fixture.database, `UPDATE games SET title='Renamed during launch',version=version+1 WHERE id=?`, request.GameID)
		}
		return original()
	}
	result, err := fixture.launcher.Create(t.Context(), "local", request)
	if err != nil || !invoked || result.LaunchID == "" {
		t.Fatalf("metadata edit hook=%v launch=%q error=%v", invoked, result.LaunchID, err)
	}
}
