package gamemetadata

import (
	"context"
	"errors"
	model "retrom/internal/model/gamemetadata"
	"testing"
	"time"
)

type candidateApplyMemory struct {
	snapshot       model.CandidateApplySnapshot
	loadErr        error
	replaceErr     error
	createErr      error
	updateErr      error
	stageErr       error
	changed        bool
	transactions   int
	replacedKinds  []string
	createdAssets  []model.CandidateAssetSelection
	metadataUpdate model.GameMetadataUpdate
	stagedIDs      []string
}

func (memory *candidateApplyMemory) WithCandidateApply(
	_ context.Context, work func(model.CandidateApplyScope) error,
) error {
	memory.transactions++
	return work(memory)
}

func (memory *candidateApplyMemory) Load(
	context.Context, string, string,
) (model.CandidateApplySnapshot, error) {
	return memory.snapshot, memory.loadErr
}

func (memory *candidateApplyMemory) ReplaceGameAssets(
	_ context.Context, _, kind string,
) ([]string, error) {
	memory.replacedKinds = append(memory.replacedKinds, kind)
	return []string{"old-blob"}, memory.replaceErr
}

func (memory *candidateApplyMemory) CreateSelectedGameAssets(
	_ context.Context, _ string, _ string, assets []model.CandidateAssetSelection, _ int64,
) ([]string, error) {
	memory.createdAssets = append(memory.createdAssets, assets...)
	if memory.createErr != nil {
		return nil, memory.createErr
	}
	return []string{"asset-id"}, nil
}

func (memory *candidateApplyMemory) UpdateGameMetadata(
	_ context.Context, update model.GameMetadataUpdate,
) (bool, error) {
	memory.metadataUpdate = update
	return memory.changed, memory.updateErr
}

func (memory *candidateApplyMemory) StageCandidates(
	_ context.Context, ids []string,
) error {
	memory.stagedIDs = append([]string(nil), ids...)
	return memory.stageErr
}

func candidateApplyService(memory *candidateApplyMemory) *Service {
	return New(memory, func() time.Time { return time.UnixMilli(1234) })
}

func TestValidCandidateFieldsRejectsUnknownAndDuplicateFields(t *testing.T) {
	for _, fields := range [][]string{{"unknown"}, {"title", "title"}} {
		if ValidCandidateFields(fields) {
			t.Fatalf("fields %v unexpectedly accepted", fields)
		}
	}
	if !ValidCandidateFields([]string{"title", "players", "releaseYear"}) {
		t.Fatal("valid candidate fields rejected")
	}
}

func TestApplyCandidateRejectsInvalidRequestBeforeTransaction(t *testing.T) {
	memory := &candidateApplyMemory{}
	_, err := candidateApplyService(memory).ApplyCandidate(t.Context(), model.ApplyCandidateRequest{GameID: "game"})
	if !errors.Is(err, model.ErrInvalid) || memory.transactions != 0 {
		t.Fatalf("error=%v transactions=%d", err, memory.transactions)
	}
}

func TestApplyCandidateCoordinatesMetadataAssetsAndPayloadStaging(t *testing.T) {
	memory := &candidateApplyMemory{
		snapshot: model.CandidateApplySnapshot{
			Version:               2,
			Current:               model.Metadata{Title: "Old", Developer: "Dev"},
			CandidateMetadataJSON: `{"title":"New","players":4,"releaseYear":null}`,
		},
		changed: true,
	}
	cover := "candidate-cover"
	result, err := candidateApplyService(memory).ApplyCandidate(t.Context(), model.ApplyCandidateRequest{
		GameID: "game", CandidateID: "candidate", ExpectedVersion: 2,
		Fields:               []string{"title", "players", "releaseYear"},
		SelectedAssets: model.SelectedAssets{CoverCandidateAssetID: &cover},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCandidateApplyResult(t, result)
	assertCandidateApplyWrites(t, memory, cover)
}

func assertCandidateApplyResult(t *testing.T, result model.ApplyCandidateResult) {
	t.Helper()
	if result.Version != 3 || result.UpdatedAtMS != 1234 || len(result.AssetIDs) != 1 ||
		result.AssetIDs[0] != "asset-id" || len(result.ReplacedBlobIDs) != 1 ||
		result.ReplacedBlobIDs[0] != "old-blob" {
		t.Fatalf("result=%+v", result)
	}
}

func assertCandidateApplyWrites(t *testing.T, memory *candidateApplyMemory, cover string) {
	t.Helper()
	if memory.metadataUpdate.Metadata.Title != "New" || memory.metadataUpdate.Metadata.Players == nil ||
		*memory.metadataUpdate.Metadata.Players != 4 || memory.metadataUpdate.Metadata.ReleaseYear != nil ||
		memory.metadataUpdate.ExpectedVersion != 2 || len(memory.createdAssets) != 1 ||
		memory.createdAssets[0].ID != cover || memory.createdAssets[0].Kind != "COVER" ||
		memory.createdAssets[0].Ordinal != 0 || len(memory.stagedIDs) != 1 ||
		memory.stagedIDs[0] != "old-blob" {
		t.Fatalf("writes=%+v created=%+v staged=%v", memory.metadataUpdate, memory.createdAssets, memory.stagedIDs)
	}
}

func TestApplyCandidateMapsStaleAndInvalidEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*candidateApplyMemory)
		want   error
	}{
		{name: "stale version", mutate: func(memory *candidateApplyMemory) { memory.snapshot.Version = 3 }, want: model.ErrCandidateStale},
		{name: "malformed metadata", mutate: func(memory *candidateApplyMemory) { memory.snapshot.CandidateMetadataJSON = "{" }, want: model.ErrCandidateMetadata},
		{name: "empty title", mutate: func(memory *candidateApplyMemory) { memory.snapshot.CandidateMetadataJSON = `{"title":"  "}` }, want: model.ErrMetadataInvalid},
		{name: "asset mismatch", mutate: func(memory *candidateApplyMemory) { memory.createErr = model.ErrCandidateAsset }, want: model.ErrCandidateAsset},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cover := "candidate-cover"
			memory := &candidateApplyMemory{
				snapshot: model.CandidateApplySnapshot{Version: 2, Current: model.Metadata{Title: "Old"}, CandidateMetadataJSON: `{"title":"New"}`},
				changed:  true,
			}
			test.mutate(memory)
			_, err := candidateApplyService(memory).ApplyCandidate(t.Context(), model.ApplyCandidateRequest{
				GameID: "game", CandidateID: "candidate", ExpectedVersion: 2, Fields: []string{"title"},
				SelectedAssets: model.SelectedAssets{CoverCandidateAssetID: &cover},
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}
