package metadatascrape

import (
	"encoding/json"
	model "retrom/internal/model/metadatascrape"
	"testing"

	"retrom/internal/adapter/metadata/hasheous"

	"github.com/google/uuid"
)

func TestMediaJobFreezesAssetIdentityAndSource(t *testing.T) {
	asset := model.CandidateAsset{
		ID: "asset", CandidateID: "candidate", ResponseID: "response", Now: 100,
		Reference: hasheous.AssetRef{ProviderAssetID: "one", Path: "/api/v1/images/one", Kind: "COVER", Ordinal: 0},
	}
	plan, err := newMediaJob(model.Subject{Kind: "IMPORT_ITEM", ID: "item"}, "run", asset)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := uuid.Parse(plan.JobID); err != nil || id.Version() != 7 {
		t.Fatalf("worker job identity=%v/%v", id, err)
	}
	var input model.MediaInputEnvelope
	if err := json.Unmarshal([]byte(plan.InputJSON), &input); err != nil {
		t.Fatal(err)
	}
	if input.Kind != "MEDIA_FETCH" || input.SchemaVersion != 1 || input.Scope.Type != "IMPORT_ITEM" || input.Scope.ID != "item" ||
		input.Inputs.AssetID != "asset" || input.Inputs.RunID != "run" || input.Inputs.ResponseID != "response" ||
		input.Inputs.SourceDigest != mediaSourceDigest(asset) || len(plan.InputDigest) != 64 || len(plan.Dedupe) != 64 {
		t.Fatalf("wrong media input: %+v", input)
	}
}
