package gameassets

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/gameassets"
)

type memoryRepository struct {
	version    int64
	exists     bool
	consumeErr error
	createCmd  *model.CreateCommand
	deleteCmd  *model.DeleteCommand
}

func (repository *memoryRepository) Upload(context.Context, string) (model.UploadedFile, bool, error) {
	return model.UploadedFile{}, false, nil
}

func (repository *memoryRepository) CommitCreate(
	_ context.Context, cmd model.CreateCommand,
) error {
	repository.createCmd = &cmd
	if cmd.ExpectedVersion != repository.version {
		return model.ErrVersionConflict
	}
	if repository.consumeErr != nil {
		return model.ErrUploadConsumed
	}
	return nil
}

func (repository *memoryRepository) CommitDelete(
	_ context.Context, cmd model.DeleteCommand,
) (model.DeleteResult, error) {
	repository.deleteCmd = &cmd
	if cmd.ExpectedVersion != repository.version {
		return model.DeleteResult{}, model.ErrVersionConflict
	}
	if !repository.exists {
		return model.DeleteResult{}, model.ErrAssetNotFound
	}
	return model.DeleteResult{Version: cmd.ExpectedVersion + 1}, nil
}

func fixedIDs(ids ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		id := ids[index]
		index++
		return id, nil
	}
}

func TestValidUploadRestrictsKindsAndOrdinals(t *testing.T) {
	for _, test := range []struct {
		kind    string
		ordinal int64
		valid   bool
	}{
		{kind: "COVER", ordinal: 0, valid: true},
		{kind: "SCREENSHOT", ordinal: 31, valid: true},
		{kind: "VIDEO", ordinal: 1, valid: false},
		{kind: "UNKNOWN", ordinal: 0, valid: false},
		{kind: "SCREENSHOT", ordinal: 32, valid: false},
	} {
		if got := ValidUpload(test.kind, test.ordinal); got != test.valid {
			t.Errorf("ValidUpload(%q, %d) = %v, want %v",
				test.kind, test.ordinal, got, test.valid)
		}
	}
}

func TestCreatePassesCommandToRepository(t *testing.T) {
	repo := &memoryRepository{version: 3}
	service := New(repo, nil, nil).WithIDFactory(
		fixedIDs("asset-id", "consumption-id"),
	)
	result, err := service.Create(t.Context(), CreateRequest{
		GameID: "game", UploadFileID: "file", Kind: "COVER",
		ExpectedVersion: 3, NowMS: 100,
		Asset: PreparedAsset{
			UploadID: "upload", BlobID: "blob", MediaType: "image/png",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AssetID != "asset-id" || result.Version != 4 || result.GameID != "game" {
		t.Fatalf("create result = %#v", result)
	}
	if repo.createCmd == nil || repo.createCmd.AssetID != "asset-id" {
		t.Fatalf("create command = %#v", repo.createCmd)
	}
}

func TestCreateMapsUploadConsumptionFailureToStableConflict(t *testing.T) {
	repo := &memoryRepository{
		version:    1,
		consumeErr: errors.New("unique constraint"),
	}
	service := New(repo, nil, nil).WithIDFactory(
		fixedIDs("asset-id", "consumption-id"),
	)
	_, err := service.Create(t.Context(), CreateRequest{
		GameID: "game", UploadFileID: "file", Kind: "VIDEO",
		ExpectedVersion: 1, NowMS: 100,
		Asset: PreparedAsset{
			UploadID: "upload", BlobID: "blob", MediaType: "video/mp4",
		},
	})
	if !errors.Is(err, model.ErrUploadConsumed) {
		t.Fatalf("create consumption error = %v", err)
	}
}

func TestDeleteRequiresExistingVideoAndCurrentVersion(t *testing.T) {
	repo := &memoryRepository{version: 2, exists: false}
	service := New(repo, nil, nil)
	_, err := service.Delete(context.Background(), DeleteRequest{
		GameID: "game", Kind: "VIDEO", ExpectedVersion: 2, NowMS: 100,
	})
	if !errors.Is(err, model.ErrAssetNotFound) {
		t.Fatalf("delete missing asset = %v", err)
	}
}
