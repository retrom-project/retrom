package payloadrelease

import (
	"context"
	"errors"
	"math"
	"reflect"
	model "retrom/internal/model/payloadrelease"
	"testing"
)

type impactMemory struct {
	snapshot model.ImpactSnapshot
	err      error
}

func (repository impactMemory) ReadImpact(context.Context, string) (model.ImpactSnapshot, error) {
	return repository.snapshot, repository.err
}

func TestImpactQueriesClassifyUniqueBlobsAndFreezeTheDigest(t *testing.T) {
	t.Parallel()
	input := model.ImpactSnapshot{
		GameID: "game", Blobs: []model.ImpactBlob{
			{ID: "exclusive", SizeBytes: 10, ProtectiveReferences: 2, GameReferences: 2},
			{ID: "shared", SizeBytes: 20, ProtectiveReferences: 3, GameReferences: 1},
			{ID: "exclusive", SizeBytes: 10, ProtectiveReferences: 2, GameReferences: 2},
		}, SourceKinds: []string{"SERVER_PEGASUS_IMPORT", "SERVER_EMULATIONSTATION_IMPORT", "IMPORT_REVIEW", "ADMIN_REPLACE"},
		Counts: model.ImpactCounts{SaveStates: 1, Assets: 2, ContentFiles: 3, ActiveLaunches: 4, ActiveNetplay: 5, ReviewEvents: 6},
	}
	result, err := model.NewImpactQueries(impactMemory{snapshot: input}).Game(t.Context(), "game")
	want := model.GameImpact{
		ImpactDigest: result.ImpactDigest, RegisteredBytes: "30", ExclusiveBytes: "10", SharedBytes: "20",
		BlobCount: 2, SaveStateCount: 1, AssetCount: 2, ContentFileCount: 3, ActiveLaunchCount: 4,
		ActiveNetplayCount: 5, ReviewEventCount: 6, SourceKinds: []string{"ADMIN_REPLACE", "SERVER_SCAN", "USER_UPLOAD"},
	}
	if err != nil || !reflect.DeepEqual(result, want) {
		t.Fatalf("wrong deletion impact: %+v %v", result, err)
	}
	input.Blobs[0], input.Blobs[1] = input.Blobs[1], input.Blobs[0]
	input.SourceKinds[0], input.SourceKinds[3] = input.SourceKinds[3], input.SourceKinds[0]
	reordered, err := model.NewImpactQueries(impactMemory{snapshot: input}).Game(t.Context(), "game")
	if err != nil || result.ImpactDigest == "" || result.ImpactDigest != reordered.ImpactDigest {
		t.Fatalf("unstable order-independent impact: %+v %+v %v", result, reordered, err)
	}
	input.Counts.ActiveLaunches++
	changed, err := model.NewImpactQueries(impactMemory{snapshot: input}).Game(t.Context(), "game")
	if err != nil || changed.ImpactDigest == result.ImpactDigest {
		t.Fatal("changed active runtime did not invalidate deletion precondition")
	}
	if _, found := model.GameDeleteAuditImpact(result)["impactDigest"]; found {
		t.Fatal("ephemeral precondition digest entered durable payload audit")
	}
}

func TestImpactQueriesRejectCorruptOrOverflowingSnapshots(t *testing.T) {
	t.Parallel()
	for name, snapshot := range map[string]model.ImpactSnapshot{
		"overflow":           {GameID: "game", Blobs: []model.ImpactBlob{{ID: "a", SizeBytes: math.MaxInt64}, {ID: "b", SizeBytes: 1}}},
		"negative-size":      {GameID: "game", Blobs: []model.ImpactBlob{{ID: "a", SizeBytes: -1}}},
		"negative-reference": {GameID: "game", Blobs: []model.ImpactBlob{{ID: "a", ProtectiveReferences: -1}}},
		"negative-count":     {GameID: "game", Counts: model.ImpactCounts{SaveStates: -1}},
		"duplicate-mismatch": {GameID: "game", Blobs: []model.ImpactBlob{{ID: "a", SizeBytes: 1}, {ID: "a", SizeBytes: 2}}},
		"scope":              {GameID: "another"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := model.NewImpactQueries(impactMemory{snapshot: snapshot}).Game(t.Context(), "game")
			if !errors.Is(err, model.ErrImpactInvalid) || !reflect.DeepEqual(result, model.GameImpact{}) {
				t.Fatalf("invalid input yielded deletion evidence: %+v %v", result, err)
			}
		})
	}
}

func TestImpactQueriesKeepStorageCauseAndNonNullSourceKinds(t *testing.T) {
	t.Parallel()
	cause := errors.New("snapshot commit failed")
	result, err := model.NewImpactQueries(impactMemory{err: cause}).Game(t.Context(), "game")
	if !errors.Is(err, cause) || !reflect.DeepEqual(result, model.GameImpact{}) {
		t.Fatalf("storage failure yielded result or lost cause: %+v %v", result, err)
	}
	result, err = model.NewImpactQueries(impactMemory{snapshot: model.ImpactSnapshot{GameID: "missing"}}).Game(t.Context(), "missing")
	if err != nil || result.SourceKinds == nil || len(result.SourceKinds) != 0 || result.RegisteredBytes != "0" {
		t.Fatalf("empty snapshot: %+v %v", result, err)
	}
}
