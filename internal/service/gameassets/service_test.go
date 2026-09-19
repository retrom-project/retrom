package gameassets

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/gameassets"
)

type memoryRepository struct {
	scope *memoryWriteScope
}

func (repository *memoryRepository) Upload(context.Context, string) (model.UploadedFile, bool, error) {
	return model.UploadedFile{}, false, nil
}

func (repository *memoryRepository) WithWrite(
	_ context.Context, work func(model.WriteScope) error,
) error {
	return work(repository.scope)
}

type memoryWriteScope struct {
	version    int64
	exists     bool
	consumeErr error
	calls      []string
}

func (scope *memoryWriteScope) GameVersion(context.Context, string) (int64, error) {
	scope.calls = append(scope.calls, "version")
	return scope.version, nil
}

func (scope *memoryWriteScope) AssetExists(context.Context, string, string) (bool, error) {
	scope.calls = append(scope.calls, "exists")
	return scope.exists, nil
}

func (scope *memoryWriteScope) RemoveSlot(context.Context, string, string, int64) ([]string, error) {
	scope.calls = append(scope.calls, "remove")
	return []string{"old-blob"}, nil
}

func (scope *memoryWriteScope) Create(context.Context, model.AssetRecord) error {
	scope.calls = append(scope.calls, "create")
	return nil
}

func (scope *memoryWriteScope) ConsumeUpload(context.Context, model.ConsumptionRecord) error {
	scope.calls = append(scope.calls, "consume")
	return scope.consumeErr
}

func (scope *memoryWriteScope) UpdateGame(context.Context, string, int64, int64) (bool, error) {
	scope.calls = append(scope.calls, "update")
	return true, nil
}

func (scope *memoryWriteScope) StageCandidates(context.Context, []string) error {
	scope.calls = append(scope.calls, "stage")
	return nil
}

func (scope *memoryWriteScope) ScheduleConsumption(context.Context, string, int64) error {
	scope.calls = append(scope.calls, "schedule")
	return nil
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
			t.Errorf("ValidUpload(%q, %d) = %v, want %v", test.kind, test.ordinal, got, test.valid)
		}
	}
}

func TestCreateUsesOneWriteScopeForReplacementAndRelease(t *testing.T) {
	scope := &memoryWriteScope{version: 3}
	service := New(&memoryRepository{scope: scope}, nil, nil).WithIDFactory(
		fixedIDs("asset-id", "consumption-id"),
	)
	result, err := service.Create(t.Context(), CreateRequest{
		GameID: "game", UploadFileID: "file", Kind: "COVER", ExpectedVersion: 3, NowMS: 100,
		Asset: PreparedAsset{UploadID: "upload", BlobID: "blob", MediaType: "image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AssetID != "asset-id" || result.Version != 4 || result.GameID != "game" {
		t.Fatalf("create result = %#v", result)
	}
	want := []string{"version", "remove", "create", "consume", "update", "stage", "schedule"}
	if len(scope.calls) != len(want) {
		t.Fatalf("write calls = %v, want %v", scope.calls, want)
	}
	for index := range want {
		if scope.calls[index] != want[index] {
			t.Fatalf("write calls = %v, want %v", scope.calls, want)
		}
	}
}

func TestCreateMapsUploadConsumptionFailureToStableConflict(t *testing.T) {
	scope := &memoryWriteScope{version: 1, consumeErr: errors.New("unique constraint")}
	service := New(&memoryRepository{scope: scope}, nil, nil).WithIDFactory(
		fixedIDs("asset-id", "consumption-id"),
	)
	_, err := service.Create(t.Context(), CreateRequest{
		GameID: "game", UploadFileID: "file", Kind: "VIDEO", ExpectedVersion: 1, NowMS: 100,
		Asset: PreparedAsset{UploadID: "upload", BlobID: "blob", MediaType: "video/mp4"},
	})
	if !errors.Is(err, ErrUploadConsumed) {
		t.Fatalf("create consumption error = %v", err)
	}
	if len(scope.calls) != 4 || scope.calls[3] != "consume" {
		t.Fatalf("writes after consumption failure = %v", scope.calls)
	}
}

func TestDeleteRequiresExistingVideoAndCurrentVersion(t *testing.T) {
	scope := &memoryWriteScope{version: 2, exists: false}
	service := New(&memoryRepository{scope: scope}, nil, nil)
	_, err := service.Delete(context.Background(), DeleteRequest{
		GameID: "game", Kind: "VIDEO", ExpectedVersion: 2, NowMS: 100,
	})
	if !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("delete missing asset = %v", err)
	}
	if len(scope.calls) != 2 || scope.calls[1] != "exists" {
		t.Fatalf("delete calls = %v", scope.calls)
	}
}
