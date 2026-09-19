package metadatascrape

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
)

func newMediaJob(
	scope metadatascrapemodel.Subject,
	runID string,
	asset metadatascrapemodel.CandidateAsset,
) (metadatascrapemodel.MediaJobPlan, error) {
	jobID, err := scheduleID()
	if err != nil {
		return metadatascrapemodel.MediaJobPlan{}, err
	}
	executionID, err := scheduleID()
	if err != nil {
		return metadatascrapemodel.MediaJobPlan{}, err
	}
	envelope := metadatascrapemodel.MediaInputEnvelope{
		SchemaVersion: 1, Kind: "MEDIA_FETCH", Scope: metadatascrapemodel.MediaInputScope{Type: scope.Kind, ID: scope.ID},
		ExecutionID: executionID, Inputs: metadatascrapemodel.MediaInput{
			AssetID: asset.ID, RunID: runID, ResponseID: asset.ResponseID, SourceDigest: mediaSourceDigest(asset),
		},
	}
	input, err := json.Marshal(envelope)
	if err != nil {
		return metadatascrapemodel.MediaJobPlan{}, fmt.Errorf("encode media execution input: %w", err)
	}
	canonical, err := json.Marshal(map[string]string{"candidateAssetId": asset.ID})
	if err != nil {
		return metadatascrapemodel.MediaJobPlan{}, fmt.Errorf("encode media job identity: %w", err)
	}
	digest := sha256.Sum256(input)
	dedupe := sha256.Sum256(append([]byte("retrom-job-dedupe-v1\x00MEDIA_FETCH\x00"), canonical...))
	return metadatascrapemodel.MediaJobPlan{
		JobID: jobID, RunID: runID, AssetID: asset.ID, Scope: scope, Now: asset.Now,
		InputJSON: string(input), InputDigest: hex.EncodeToString(digest[:]), Dedupe: hex.EncodeToString(dedupe[:]),
	}, nil
}

func mediaSourceDigest(asset metadatascrapemodel.CandidateAsset) string {
	source := sha256.New()
	for _, field := range []string{
		asset.ID, asset.CandidateID, asset.ResponseID, asset.Reference.ProviderAssetID,
		asset.Reference.Path, asset.Reference.Kind, strconv.Itoa(asset.Reference.Ordinal),
	} {
		_, _ = fmt.Fprintf(source, "%d:%s", len(field), field)
	}
	return hex.EncodeToString(source.Sum(nil))
}

func enqueueCandidateMedia(
	ctx context.Context,
	scope metadatascrapemodel.ResultScope,
	runID string,
	asset *metadatascrapemodel.CandidateAsset,
) error {
	subject, err := scope.Read.Subject(ctx, runID)
	if err != nil {
		return fmt.Errorf("read media owner: %w", err)
	}
	plan, err := newMediaJob(subject, runID, *asset)
	if err != nil {
		return err
	}
	if err := scope.Media.Enqueue(ctx, plan); err != nil {
		return fmt.Errorf("enqueue media fetch: %w", err)
	}
	asset.MediaJobID = plan.JobID
	return nil
}
