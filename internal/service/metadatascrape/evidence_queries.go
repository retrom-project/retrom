package metadatascrape

import (
	"context"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/metadatascrape"
)

type EvidenceQueries struct{ reader model.ReviewEvidenceReader }

func NewEvidenceQueries(reader model.ReviewEvidenceReader) *EvidenceQueries {
	return &EvidenceQueries{reader: reader}
}

func (service *EvidenceQueries) Review(ctx context.Context, itemID string) (model.ReviewEvidence, error) {
	records, err := service.reader.ReviewCandidates(ctx, itemID)
	if err != nil {
		return model.ReviewEvidence{}, fmt.Errorf("read review candidates: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	assets, err := service.Assets(ctx, ids)
	if err != nil {
		return model.ReviewEvidence{}, err
	}
	byCandidate := make(map[string][]model.CandidateAssetView, len(ids))
	for _, asset := range assets {
		byCandidate[asset.CandidateID] = append(byCandidate[asset.CandidateID], asset)
	}
	result := model.ReviewEvidence{Candidates: make([]model.ReviewCandidate, 0, len(records))}
	for _, record := range records {
		metadata, err := decodeReviewCandidateDocument(record.MetadataJSON)
		if err != nil {
			return model.ReviewEvidence{}, err
		}
		evidence, err := decodeReviewCandidateDocument(record.EvidenceJSON)
		if err != nil {
			return model.ReviewEvidence{}, err
		}
		selected := byCandidate[record.ID]
		if selected == nil {
			selected = []model.CandidateAssetView{}
		}
		result.Candidates = append(result.Candidates,
			model.ReviewCandidate{
				ID:             record.ID,
				RunID:          record.RunID,
				ProviderGameID: record.ProviderGameID,
				Metadata:       metadata,
				Evidence:       evidence,
				Assets:         selected,
				CreatedAtMS:    record.CreatedAtMS,
			})
	}
	result.Runs, err = service.reader.ReviewRuns(ctx, itemID)
	if err != nil {
		return model.ReviewEvidence{}, fmt.Errorf("read review scrape runs: %w", err)
	}
	if result.Runs == nil {
		result.Runs = []model.ReviewRun{}
	}
	return result, nil
}

func (service *EvidenceQueries) Assets(ctx context.Context, ids []string) ([]model.CandidateAssetView, error) {
	if len(ids) == 0 {
		return []model.CandidateAssetView{}, nil
	}
	assets, err := service.reader.CandidateAssets(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read candidate assets: %w", err)
	}
	if assets == nil {
		assets = []model.CandidateAssetView{}
	}
	return assets, nil
}

func decodeReviewCandidateDocument(raw string) (json.RawMessage, error) {
	var result json.RawMessage
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("decode review candidate document: %w", err)
	}
	return result, nil
}
