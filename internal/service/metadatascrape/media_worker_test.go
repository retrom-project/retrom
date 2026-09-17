package metadatascrape

import (
	"encoding/json"
	"errors"
	"testing"

	model "retrom/internal/model/metadatascrape"
)

func TestMediaInputRejectsChangedSourceAndPreservesJSONCause(t *testing.T) {
	asset := model.CandidateAsset{ID: "asset", CandidateID: "candidate", ResponseID: "response"}
	plan, err := newMediaJob(model.Subject{Kind: "GAME", ID: "game"}, "run", asset)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.MediaSnapshot{
		Job:   model.MediaJob{ID: plan.JobID, Scope: plan.Scope, Input: plan.InputJSON, InputDigest: plan.InputDigest},
		Asset: model.MediaAsset{CandidateAsset: asset, RunID: "run"},
	}
	if err := model.ValidateMediaInput(snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Asset.Reference.Path = "changed"
	if err := model.ValidateMediaInput(snapshot); !errors.Is(err, model.ErrMediaInput) {
		t.Fatalf("changed source accepted: %v", err)
	}
	snapshot.Job.Input = "{"
	snapshot.Job.InputDigest = model.MediaDigest(snapshot.Job.Input)
	var syntax *json.SyntaxError
	if err := model.ValidateMediaInput(snapshot); !errors.Is(err, model.ErrMediaInput) || !errors.As(err, &syntax) {
		t.Fatalf("lost JSON cause: %v", err)
	}
}

func TestMediaOrderingUsesCandidateRankingThenAssetIdentity(t *testing.T) {
	assets := []model.MediaOrder{
		{ID: "d", Hits: 1, QueryOrder: 1, GameID: "b", Kind: "COVER"},
		{ID: "b", Hits: 2, QueryOrder: 2, GameID: "a", Kind: "SCREENSHOT"},
		{ID: "a", Hits: 2, QueryOrder: 1, GameID: "z", Kind: "COVER"},
		{ID: "c", Hits: 2, QueryOrder: 2, GameID: "a", Kind: "COVER"},
	}
	model.SortMedia(assets)
	for index, id := range []string{"a", "c", "b", "d"} {
		if assets[index].ID != id {
			t.Fatalf("order=%+v", assets)
		}
	}
}

func TestMediaCompletionDistinguishesPersistedAndCallerDeadlines(t *testing.T) {
	for _, test := range []struct {
		name     string
		deadline int64
		code     string
	}{
		{"execution expired", 100, "MEDIA_EXECUTION_EXPIRED"}, {"caller expired", 200, "MEDIA_EXECUTION_INTERRUPTED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory, err := newMediaMemory()
			if err != nil {
				t.Fatal(err)
			}
			snapshot := memory.snapshot
			snapshot.Job.State = "RUNNING"
			snapshot.Job.WorkerID = "worker"
			snapshot.Job.Attempt = 1
			snapshot.Job.LeaseUntil = 100
			snapshot.Job.Deadline = test.deadline
			claim := model.MediaClaim{JobID: snapshot.Job.ID, WorkerID: "worker", Execution: 1, Attempt: 1}
			outcome := model.MediaCompletion(snapshot, model.MediaSettleCommand{
				Claim: claim, Failed: true, Retryable: true, Now: 100,
			})
			if outcome.Code != test.code {
				t.Fatalf("deadline=%d outcome=%+v", test.deadline, outcome)
			}
		})
	}
}

func TestMediaGlobalCapacityDoesNotClaimOrSpendAttempt(t *testing.T) {
	memory, err := newMediaMemory()
	if err != nil {
		t.Fatal(err)
	}
	memory.running = 2
	source := &assetFetcher{}
	if err := NewMediaWorker(memory, source, &assetBytes{}, mediaUnitNow).Run(t.Context(), memory.snapshot.Job.ID); err != nil {
		t.Fatal(err)
	}
	if source.calls != 0 || memory.snapshot.Job.Attempt != 0 || memory.snapshot.Charged != 0 {
		t.Fatalf("full instance started media: calls=%d snapshot=%+v", source.calls, memory.snapshot)
	}
}
