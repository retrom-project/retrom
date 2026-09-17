package gamemetadata

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/gamemetadata"
)

type candidateApplyMemory struct {
	snapshot     model.CandidateApplySnapshot
	loadErr      error
	commitErr    error
	changed      bool
	loadCalls    int
	commitCmd    model.CandidateApplyCommand
	commitResult model.CandidateApplyCommitResult
}

func (memory *candidateApplyMemory) LoadCandidateApplySnapshot(
	_ context.Context, _, _ string,
) (model.CandidateApplySnapshot, error) {
	memory.loadCalls++
	return memory.snapshot, memory.loadErr
}

func (memory *candidateApplyMemory) CommitCandidateApply(
	_ context.Context, cmd model.CandidateApplyCommand,
) (model.CandidateApplyCommitResult, error) {
	memory.commitCmd = cmd
	if memory.commitErr != nil {
		return model.CandidateApplyCommitResult{}, memory.commitErr
	}
	if !memory.changed {
		return model.CandidateApplyCommitResult{}, model.ErrVersionConflict
	}
	return memory.commitResult, nil
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
	if !errors.Is(err, model.ErrInvalid) || memory.loadCalls != 0 {
		t.Fatalf("error=%v loadCalls=%d", err, memory.loadCalls)
	}
}

func TestApplyCandidateCoordinatesMetadataAssetsAndPayloadStaging(t *testing.T) {
	cover := "candidate-cover"
	memory := &candidateApplyMemory{
		snapshot: model.CandidateApplySnapshot{
			Version:               2,
			Current:               model.Metadata{Title: "Old", Developer: "Dev"},
			CandidateMetadataJSON: `{"title":"New","players":4,"releaseYear":null}`,
		},
		changed: true,
		commitResult: model.CandidateApplyCommitResult{
			ReplacedBlobIDs: []string{"old-blob"},
			AssetIDs:        []string{"asset-id"},
		},
	}
	result, err := candidateApplyService(memory).ApplyCandidate(t.Context(), model.ApplyCandidateRequest{
		GameID: "game", CandidateID: "candidate", ExpectedVersion: 2,
		Fields:         []string{"title", "players", "releaseYear"},
		SelectedAssets: model.SelectedAssets{CoverCandidateAssetID: &cover},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 3 || result.UpdatedAtMS != 1234 || len(result.AssetIDs) != 1 ||
		result.AssetIDs[0] != "asset-id" || len(result.ReplacedBlobIDs) != 1 ||
		result.ReplacedBlobIDs[0] != "old-blob" {
		t.Fatalf("result=%+v", result)
	}
	cmd := memory.commitCmd
	if cmd.Metadata.Title != "New" || cmd.Metadata.Players == nil ||
		*cmd.Metadata.Players != 4 || cmd.Metadata.ReleaseYear != nil ||
		cmd.ExpectedVersion != 2 {
		t.Fatalf("cmd=%+v", cmd)
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
		{name: "asset mismatch", mutate: func(memory *candidateApplyMemory) { memory.commitErr = model.ErrCandidateAsset }, want: model.ErrCandidateAsset},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cover := "candidate-cover"
			memory := &candidateApplyMemory{
				snapshot: model.CandidateApplySnapshot{
					Version: 2, Current: model.Metadata{Title: "Old"},
					CandidateMetadataJSON: `{"title":"New"}`,
				},
				changed: true,
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
