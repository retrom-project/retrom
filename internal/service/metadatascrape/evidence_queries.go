package metadatascrape

import (
	"context"
	"encoding/json"
	"fmt"
)

type EvidenceQueries struct{ reader ReviewEvidenceReader }

func NewEvidenceQueries(reader ReviewEvidenceReader) *EvidenceQueries {
	return &EvidenceQueries{reader: reader}
}

func (service *EvidenceQueries) Review(ctx context.Context, itemID string) (ReviewEvidence, error) {
	records, err := service.reader.ReviewCandidates(ctx, itemID)
	if err != nil {
		return ReviewEvidence{}, fmt.Errorf("read review candidates: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	assets, err := service.Assets(ctx, ids)
	if err != nil {
		return ReviewEvidence{}, err
	}
	byCandidate := make(map[string][]CandidateAssetView, len(ids))
	for _, asset := range assets {
		byCandidate[asset.CandidateID] = append(byCandidate[asset.CandidateID], asset)
	}
	result := ReviewEvidence{Candidates: make([]ReviewCandidate, 0, len(records))}
	for _, record := range records {
		metadata, err := decodeReviewCandidateDocument(record.MetadataJSON)
		if err != nil {
			return ReviewEvidence{}, err
		}
		evidence, err := decodeReviewCandidateDocument(record.EvidenceJSON)
		if err != nil {
			return ReviewEvidence{}, err
		}
		selected := byCandidate[record.ID]
		if selected == nil {
			selected = []CandidateAssetView{}
		}
		result.Candidates = append(result.Candidates,
			ReviewCandidate{
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
		return ReviewEvidence{}, fmt.Errorf("read review scrape runs: %w", err)
	}
	if result.Runs == nil {
		result.Runs = []ReviewRun{}
	}
	return result, nil
}

func (service *EvidenceQueries) Assets(ctx context.Context, ids []string) ([]CandidateAssetView, error) {
	if len(ids) == 0 {
		return []CandidateAssetView{}, nil
	}
	assets, err := service.reader.CandidateAssets(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read candidate assets: %w", err)
	}
	if assets == nil {
		assets = []CandidateAssetView{}
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
