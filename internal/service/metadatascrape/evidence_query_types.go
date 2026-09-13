package metadatascrape

import (
	"context"
	"encoding/json"
)

type ReviewEvidenceReader interface {
	ReviewCandidates(context.Context, string) ([]ReviewCandidateRecord, error)
	CandidateAssets(context.Context, []string) ([]CandidateAssetView, error)
	ReviewRuns(context.Context, string) ([]ReviewRun, error)
}
type ReviewCandidateRecord struct {
	ID, RunID, ProviderGameID, MetadataJSON, EvidenceJSON string
	CreatedAtMS                                           int64
}
type CandidateAssetView struct {
	CandidateID     string  `json:"-"`
	ID              string  `json:"candidateAssetId"`
	ProviderAssetID string  `json:"providerAssetId"`
	Kind            string  `json:"kind"`
	Ordinal         int64   `json:"ordinal"`
	Status          string  `json:"status"`
	WidthPX         *int64  `json:"widthPx"`
	HeightPX        *int64  `json:"heightPx"`
	MediaType       *string `json:"mediaType"`
	ErrorCode       *string `json:"errorCode"`
}
type ReviewCandidate struct {
	ID             string               `json:"candidateId"`
	RunID          string               `json:"scrapeRunId"`
	ProviderGameID string               `json:"providerGameId"`
	Metadata       json.RawMessage      `json:"metadata"`
	Evidence       json.RawMessage      `json:"evidence"`
	Assets         []CandidateAssetView `json:"assets"`
	CreatedAtMS    int64                `json:"createdAtMs"`
}
type ReviewRunOutcomes struct {
	Hit             int64 `json:"hit"`
	Miss            int64 `json:"miss"`
	RateLimited     int64 `json:"rateLimited"`
	Timeout         int64 `json:"timeout"`
	InvalidResponse int64 `json:"invalidResponse"`
	NetworkError    int64 `json:"networkError"`
}
type ReviewRun struct {
	ID             string            `json:"scrapeRunId"`
	JobID          string            `json:"jobId"`
	Provider       string            `json:"provider"`
	State          string            `json:"state"`
	JobState       string            `json:"jobState"`
	CreatedAtMS    int64             `json:"createdAtMs"`
	CompletedAtMS  *int64            `json:"completedAtMs"`
	ErrorCode      *string           `json:"errorCode"`
	EvidenceCount  int64             `json:"evidenceCount"`
	AttemptCount   int64             `json:"attemptCount"`
	CandidateCount int64             `json:"candidateCount"`
	Outcomes       ReviewRunOutcomes `json:"outcomes"`
}
type ReviewEvidence struct {
	Candidates []ReviewCandidate
	Runs       []ReviewRun
}
