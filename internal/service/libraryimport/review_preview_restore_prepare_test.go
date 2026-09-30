package libraryimport

import (
	"context"
	"errors"
	"testing"

	launch "retrom/internal/service/launch"
)

func TestPreparedRestoreRejectsChangedCheckpointAndDiscardsCopiedPayload(t *testing.T) {
	t.Parallel()
	creator, repository, _, request := previewFixture(t)
	original := "original"
	request.RestoreFromPreviewID = &original
	repository.restore = previewFixtureRestore(repository)
	creator.environment.CopyRestorePayload = func(_ context.Context, _, record string) (string, error) {
		repository.restore.FileRecord = "saved-C"
		return "copy-" + record, nil
	}
	discarded := false
	creator.environment.DiscardPreviewPayload = func(_ context.Context, id string) error {
		if repository.inTransaction || id != previewTestID {
			t.Fatal("invalid prepared restore cleanup")
		}
		discarded = true
		return nil
	}
	result, err := creator.Create(t.Context(), request)
	if !errors.Is(err, launch.ErrSaveIncompatible) || result.PreviewID != "" || len(repository.writes) != 0 || !discarded {
		t.Fatalf("changed checkpoint committed: result=%+v discarded=%v err=%v", result, discarded, err)
	}
}

func TestRestoreCopyFailureStopsBeforeCreationTransaction(t *testing.T) {
	t.Parallel()
	creator, repository, _, request := previewFixture(t)
	original := "original"
	request.RestoreFromPreviewID = &original
	repository.restore = previewFixtureRestore(repository)
	cause := errors.New("restore copy failed")
	creator.environment.CopyRestorePayload = func(context.Context, string, string) (string, error) { return "", cause }
	result, err := creator.Create(t.Context(), request)
	if !errors.Is(err, cause) || result.PreviewID != "" || repository.transactions != 0 || len(repository.writes) != 0 {
		t.Fatalf("failed copy reached writes: result=%+v transactions=%d err=%v", result, repository.transactions, err)
	}
}
