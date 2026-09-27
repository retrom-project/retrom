package gamemetadata

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/filestore"
)

type candidateApplyMemory struct {
	files          *filestore.Store
	snapshot       CandidateApplySnapshot
	loadErr        error
	replaceErr     error
	createErr      error
	updateErr      error
	stageErr       error
	changed        bool
	transactions   int
	replacedKinds  []string
	createdAssets  []CandidateAssetSelection
	metadataUpdate GameMetadataUpdate
	stagedIDs      []string
}

func (memory *candidateApplyMemory) WithCandidateApply(
	_ context.Context, work func(CandidateApplyScope) error,
) error {
	memory.transactions++
	return work(memory)
}

func (memory *candidateApplyMemory) Load(
	context.Context, string, string,
) (CandidateApplySnapshot, error) {
	return memory.snapshot, memory.loadErr
}

func (memory *candidateApplyMemory) ReplaceGameAssets(
	_ context.Context, _, kind string,
) ([]string, error) {
	memory.replacedKinds = append(memory.replacedKinds, kind)
	return []string{"old-blob"}, memory.replaceErr
}

func (memory *candidateApplyMemory) CreateSelectedGameAssets(
	_ context.Context, _ string, _ string, assets []CandidateAssetSelection, _ int64,
) ([]string, error) {
	memory.createdAssets = append(memory.createdAssets, assets...)
	if memory.createErr != nil {
		return nil, memory.createErr
	}
	return []string{"asset-id"}, nil
}

func (memory *candidateApplyMemory) UpdateGameMetadata(
	_ context.Context, update GameMetadataUpdate,
) (bool, error) {
	memory.metadataUpdate = update
	return memory.changed, memory.updateErr
}

func (memory *candidateApplyMemory) StageCandidates(
	_ context.Context, _ string, ids []string, _ int64,
) error {
	memory.stagedIDs = append([]string(nil), ids...)
	return memory.stageErr
}

func candidateApplyService(t *testing.T, memory *candidateApplyMemory) *Service {
	t.Helper()
	var err error
	memory.files, err = filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(memory, func() time.Time { return time.UnixMilli(1234) }).WithFileStore(memory.files)
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
	_, err := candidateApplyService(t, memory).ApplyCandidate(t.Context(),
		ApplyCandidateRequest{GameID: "018fbe68-0000-7000-8000-000000000001"})
	if !errors.Is(err, ErrInvalid) || memory.transactions != 0 {
		t.Fatalf("error=%v transactions=%d", err, memory.transactions)
	}
}

func TestApplyCandidateCoordinatesMetadataAssetsAndPayloadStaging(t *testing.T) {
	memory := &candidateApplyMemory{
		snapshot: CandidateApplySnapshot{
			Version:               2,
			Current:               Metadata{Title: "Old", Developer: "Dev"},
			CandidateMetadataJSON: `{"title":"New","players":4,"releaseYear":null}`,
		},
		changed: true,
	}
	cover := "candidate-cover"
	result, err := candidateApplyService(t, memory).ApplyCandidate(t.Context(), ApplyCandidateRequest{
		GameID: "018fbe68-0000-7000-8000-000000000001", CandidateID: "candidate", ExpectedVersion: 2,
		Fields:         []string{"title", "players", "releaseYear"},
		SelectedAssets: SelectedAssets{CoverCandidateAssetID: &cover},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCandidateApplyResult(t, result)
	assertCandidateApplyWrites(t, memory, cover)
}

func assertCandidateApplyResult(t *testing.T, result ApplyCandidateResult) {
	t.Helper()
	if result.Version != 3 || result.UpdatedAtMS != 1234 || len(result.AssetIDs) != 1 ||
		result.AssetIDs[0] != "asset-id" || len(result.ReplacedFileRecords) != 1 ||
		result.ReplacedFileRecords[0] != "old-blob" {
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
		{
			name:   "stale version",
			mutate: func(memory *candidateApplyMemory) { memory.snapshot.Version = 3 }, want: ErrCandidateStale,
		},
		{
			name:   "malformed metadata",
			mutate: func(memory *candidateApplyMemory) { memory.snapshot.CandidateMetadataJSON = "{" },
			want:   ErrCandidateMetadata,
		},
		{name: "empty title", mutate: func(memory *candidateApplyMemory) { memory.snapshot.CandidateMetadataJSON = `{"title":"  "}` }, want: ErrMetadataInvalid},
		{
			name:   "asset mismatch",
			mutate: func(memory *candidateApplyMemory) { memory.createErr = ErrCandidateAsset },
			want:   ErrCandidateAsset,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cover := "candidate-cover"
			memory := &candidateApplyMemory{
				snapshot: CandidateApplySnapshot{Version: 2, Current: Metadata{Title: "Old"}, CandidateMetadataJSON: `{"title":"New"}`},
				changed:  true,
			}
			test.mutate(memory)
			_, err := candidateApplyService(t, memory).ApplyCandidate(t.Context(), ApplyCandidateRequest{
				GameID: "018fbe68-0000-7000-8000-000000000001", CandidateID: "candidate",
				ExpectedVersion: 2, Fields: []string{"title"},
				SelectedAssets: SelectedAssets{CoverCandidateAssetID: &cover},
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}

func (memory *candidateApplyMemory) SelectedFiles(_ context.Context, _ string,
	selected []CandidateAssetSelection,
) ([]CandidateAssetSelection, error) {
	for i := range selected {
		file, err := memory.files.Put(strings.NewReader(selected[i].ID))
		if err != nil {
			return nil, err
		}
		selected[i].SourceFile = file.Record
	}
	return selected, nil
}
