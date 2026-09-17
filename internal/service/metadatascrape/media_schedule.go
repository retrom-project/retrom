package metadatascrape

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

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
			AssetID: asset.ID, RunID: runID, ResponseID: asset.ResponseID, SourceDigest: model.MediaSourceDigest(asset),
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
