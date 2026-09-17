package libraryimport

import (
	"encoding/json"
	"errors"
	model "retrom/internal/model/libraryimport"
	"testing"
	"time"
)

func esSourceOwnershipFixture() (model.SourceCreationIntent, model.SourceCreationSnapshot) {
	intent, before := sourceOwnershipFixture()
	intent.Kind = model.SourceOwnerEmulationStation
	before.Kind = intent.Kind
	before.Frozen = model.SourceCreationFrozen{
		RootID: "root", RootDigest: "digest", RelativePath: "games", ActorUserID: "actor", TagSnapshotJSON: `[{"tagId":"a","name":"A"},{"tagId":"b","name":"B"}]`,
		ContentKind: "SINGLE_FILE", CollectionID: "collection", MappingVersion: 1, MaxAttempts: 4, StartedAtMS: 1, ReleaseYearMax: 2033,
	}
	return intent, before
}

func TestOwnedSourceRequestUsesFrozenActorTagsAndContentMode(t *testing.T) {
	_, before := esSourceOwnershipFixture()
	request := model.OwnedServerSourceRequest{AssignedByUserID: "actor", TagIDs: []string{"b", "a"}, ContentMode: "STANDARD"}
	if err := ValidateOwnedSourceRequest(before, request); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*model.OwnedServerSourceRequest){
		"actor":        func(input *model.OwnedServerSourceRequest) { input.AssignedByUserID = "other" },
		"missing tag":  func(input *model.OwnedServerSourceRequest) { input.TagIDs = []string{"a"} },
		"repeated tag": func(input *model.OwnedServerSourceRequest) { input.TagIDs = []string{"a", "a", "b"} },
		"mode":         func(input *model.OwnedServerSourceRequest) { input.ContentMode = "MULTI_DISC" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			input := request
			change(&input)
			if err := ValidateOwnedSourceRequest(before, input); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	before.Frozen.TagSnapshotJSON = "broken"
	var syntax *json.SyntaxError
	if err := ValidateOwnedSourceRequest(before, request); !errors.As(err, &syntax) {
		t.Fatalf("decode cause lost: %v", err)
	}
}

func TestSourceOwnershipRechecksFrozenESPlanButAllowsLeaseRenewal(t *testing.T) {
	intent, before := esSourceOwnershipFixture()
	service := NewSourceOwnership(func() time.Time { return time.UnixMilli(10) })
	fields := map[string]func(*model.SourceCreationFrozen){
		"root":          func(frozen *model.SourceCreationFrozen) { frozen.RootID = "other" },
		"digest":        func(frozen *model.SourceCreationFrozen) { frozen.RootDigest = "other" },
		"relative path": func(frozen *model.SourceCreationFrozen) { frozen.RelativePath = "other" },
		"actor":         func(frozen *model.SourceCreationFrozen) { frozen.ActorUserID = "other" },
		"tags":          func(frozen *model.SourceCreationFrozen) { frozen.TagSnapshotJSON = "[]" },
		"year":          func(frozen *model.SourceCreationFrozen) { frozen.ReleaseYearMax++ },
		"mapping":       func(frozen *model.SourceCreationFrozen) { frozen.MappingVersion++ },
	}
	for name, change := range fields {
		t.Run(name, func(t *testing.T) {
			after := before
			change(&after.Frozen)
			result, err := service.Revalidate(t.Context(), &sourceRecordsStub{snapshot: after}, intent, before, "target")
			if !errors.Is(err, model.ErrVersionConflict) || result.ItemID != "" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	renewed := before
	renewed.JobVersion++
	renewed.LeaseUntilMS++
	if _, err := service.Revalidate(t.Context(), &sourceRecordsStub{snapshot: renewed}, intent, before, "target"); err != nil {
		t.Fatal(err)
	}
}
