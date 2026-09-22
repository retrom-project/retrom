package payloadrelease

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
)

type impactMemory struct {
	snapshot ImpactSnapshot
	err      error
}

func (repository impactMemory) ReadImpact(context.Context, string) (ImpactSnapshot, error) {
	return repository.snapshot, repository.err
}

func TestImpactQueriesClassifyUniqueBlobsAndFreezeTheDigest(t *testing.T) {
	t.Parallel()
	input := ImpactSnapshot{
		GameID: "game", Blobs: []ImpactBlob{
			{ID: "exclusive", SizeBytes: 10, ProtectiveReferences: 2, GameReferences: 2},
			{ID: "shared", SizeBytes: 20, ProtectiveReferences: 3, GameReferences: 1},
			{ID: "exclusive", SizeBytes: 10, ProtectiveReferences: 2, GameReferences: 2},
		}, SourceKinds: []string{"IMPORT_RECEIVE", "IMPORT_RECEIVE", "IMPORT_REVIEW", "ADMIN_REPLACE"},
		Counts: ImpactCounts{SaveStates: 1, Assets: 2, ContentFiles: 3, ActiveLaunches: 4, ActiveNetplay: 5},
	}
	result, err := NewImpactQueries(impactMemory{snapshot: input}).Game(t.Context(), "game")
	want := GameImpact{
		ImpactDigest: result.ImpactDigest, RegisteredBytes: "30", ExclusiveBytes: "10", SharedBytes: "20",
		BlobCount: 2, SaveStateCount: 1, AssetCount: 2, ContentFileCount: 3, ActiveLaunchCount: 4,
		ActiveNetplayCount: 5, SourceKinds: []string{"ADMIN_REPLACE", "SERVER_SCAN", "USER_UPLOAD"},
	}
	if err != nil || !reflect.DeepEqual(result, want) {
		t.Fatalf("wrong deletion impact: %+v %v", result, err)
	}
	input.Blobs[0], input.Blobs[1] = input.Blobs[1], input.Blobs[0]
	input.SourceKinds[0], input.SourceKinds[3] = input.SourceKinds[3], input.SourceKinds[0]
	reordered, err := NewImpactQueries(impactMemory{snapshot: input}).Game(t.Context(), "game")
	if err != nil || result.ImpactDigest == "" || result.ImpactDigest != reordered.ImpactDigest {
		t.Fatalf("unstable order-independent impact: %+v %+v %v", result, reordered, err)
	}
	input.Counts.ActiveLaunches++
	changed, err := NewImpactQueries(impactMemory{snapshot: input}).Game(t.Context(), "game")
	if err != nil || changed.ImpactDigest == result.ImpactDigest {
		t.Fatal("changed active runtime did not invalidate deletion precondition")
	}
	if _, found := GameDeleteAuditImpact(result)["impactDigest"]; found {
		t.Fatal("ephemeral precondition digest entered durable payload audit")
	}
}

func TestImpactQueriesRejectCorruptOrOverflowingSnapshots(t *testing.T) {
	t.Parallel()
	for name, snapshot := range map[string]ImpactSnapshot{
		"overflow":           {GameID: "game", Blobs: []ImpactBlob{{ID: "a", SizeBytes: math.MaxInt64}, {ID: "b", SizeBytes: 1}}},
		"negative-size":      {GameID: "game", Blobs: []ImpactBlob{{ID: "a", SizeBytes: -1}}},
		"negative-reference": {GameID: "game", Blobs: []ImpactBlob{{ID: "a", ProtectiveReferences: -1}}},
		"negative-count":     {GameID: "game", Counts: ImpactCounts{SaveStates: -1}},
		"duplicate-mismatch": {GameID: "game", Blobs: []ImpactBlob{{ID: "a", SizeBytes: 1}, {ID: "a", SizeBytes: 2}}},
		"scope":              {GameID: "another"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := NewImpactQueries(impactMemory{snapshot: snapshot}).Game(t.Context(), "game")
			if !errors.Is(err, ErrImpactInvalid) || !reflect.DeepEqual(result, GameImpact{}) {
				t.Fatalf("invalid input yielded deletion evidence: %+v %v", result, err)
			}
		})
	}
}

func TestImpactQueriesKeepStorageCauseAndNonNullSourceKinds(t *testing.T) {
	t.Parallel()
	cause := errors.New("snapshot commit failed")
	result, err := NewImpactQueries(impactMemory{err: cause}).Game(t.Context(), "game")
	if !errors.Is(err, cause) || !reflect.DeepEqual(result, GameImpact{}) {
		t.Fatalf("storage failure yielded result or lost cause: %+v %v", result, err)
	}
	result, err = NewImpactQueries(impactMemory{snapshot: ImpactSnapshot{GameID: "missing"}}).Game(t.Context(), "missing")
	if err != nil || result.SourceKinds == nil || len(result.SourceKinds) != 0 || result.RegisteredBytes != "0" {
		t.Fatalf("empty snapshot: %+v %v", result, err)
	}
}
