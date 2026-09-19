package metadatascrape

import (
	"context"
	"encoding/json"
	"fmt"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

type EvidenceQueries struct {
	reader metadatascrapemodel.ReviewEvidenceReader
}

func NewEvidenceQueries(reader metadatascrapemodel.ReviewEvidenceReader) *EvidenceQueries {
	return &EvidenceQueries{reader: reader}
}

func (service *EvidenceQueries) Review(ctx context.Context, itemID string) (metadatascrapemodel.ReviewEvidence, error) {
	records, err := service.reader.ReviewCandidates(ctx, itemID)
	if err != nil {
		return metadatascrapemodel.ReviewEvidence{}, fmt.Errorf("read review candidates: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	assets, err := service.Assets(ctx, ids)
	if err != nil {
		return metadatascrapemodel.ReviewEvidence{}, err
	}
	byCandidate := make(map[string][]metadatascrapemodel.CandidateAssetView, len(ids))
	for _, asset := range assets {
		byCandidate[asset.CandidateID] = append(byCandidate[asset.CandidateID], asset)
	}
	result := metadatascrapemodel.ReviewEvidence{Candidates: make([]metadatascrapemodel.ReviewCandidate, 0, len(records))}
	for _, record := range records {
		metadata, err := decodeReviewCandidateDocument(record.MetadataJSON)
		if err != nil {
			return metadatascrapemodel.ReviewEvidence{}, err
		}
		evidence, err := decodeReviewCandidateDocument(record.EvidenceJSON)
		if err != nil {
			return metadatascrapemodel.ReviewEvidence{}, err
		}
		selected := byCandidate[record.ID]
		if selected == nil {
			selected = []metadatascrapemodel.CandidateAssetView{}
		}
		result.Candidates = append(result.Candidates,
			metadatascrapemodel.ReviewCandidate{
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
		return metadatascrapemodel.ReviewEvidence{}, fmt.Errorf("read review scrape runs: %w", err)
	}
	if result.Runs == nil {
		result.Runs = []metadatascrapemodel.ReviewRun{}
	}
	return result, nil
}

func (service *EvidenceQueries) Assets(
	ctx context.Context,
	ids []string,
) ([]metadatascrapemodel.CandidateAssetView, error) {
	if len(ids) == 0 {
		return []metadatascrapemodel.CandidateAssetView{}, nil
	}
	assets, err := service.reader.CandidateAssets(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read candidate assets: %w", err)
	}
	if assets == nil {
		assets = []metadatascrapemodel.CandidateAssetView{}
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
