package metadatascrape

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	model "retrom/internal/model/metadatascrape"
)

func newMediaJob(scope model.Subject, runID string, asset model.CandidateAsset) (model.MediaJobPlan, error) {
	jobID, err := scheduleID()
	if err != nil {
		return model.MediaJobPlan{}, err
	}
	executionID, err := scheduleID()
	if err != nil {
		return model.MediaJobPlan{}, err
	}
	envelope := model.MediaInputEnvelope{
		SchemaVersion: 1, Kind: "MEDIA_FETCH", Scope: model.MediaInputScope{Type: scope.Kind, ID: scope.ID},
		ExecutionID: executionID, Inputs: model.MediaInput{
			AssetID: asset.ID, RunID: runID, ResponseID: asset.ResponseID, SourceDigest: mediaSourceDigest(asset),
		},
	}
	input, err := json.Marshal(envelope)
	if err != nil {
		return model.MediaJobPlan{}, fmt.Errorf("encode media execution input: %w", err)
	}
	canonical, err := json.Marshal(map[string]string{"candidateAssetId": asset.ID})
	if err != nil {
		return model.MediaJobPlan{}, fmt.Errorf("encode media job identity: %w", err)
	}
	digest := sha256.Sum256(input)
	dedupe := sha256.Sum256(append([]byte("retrom-job-dedupe-v1\x00MEDIA_FETCH\x00"), canonical...))
	return model.MediaJobPlan{
		JobID: jobID, RunID: runID, AssetID: asset.ID, Scope: scope, Now: asset.Now,
		InputJSON: string(input), InputDigest: hex.EncodeToString(digest[:]), Dedupe: hex.EncodeToString(dedupe[:]),
	}, nil
}

func mediaSourceDigest(asset model.CandidateAsset) string {
	source := sha256.New()
	for _, field := range []string{
		asset.ID, asset.CandidateID, asset.ResponseID, asset.Reference.ProviderAssetID,
		asset.Reference.Path, asset.Reference.Kind, strconv.Itoa(asset.Reference.Ordinal),
	} {
		_, _ = fmt.Fprintf(source, "%d:%s", len(field), field)
	}
	return hex.EncodeToString(source.Sum(nil))
}

func enqueueCandidateMedia(ctx context.Context, scope model.ResultScope, runID string, asset *model.CandidateAsset) error {
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
