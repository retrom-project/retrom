package serverimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/serverimport"
)

type creationMemory struct {
	entries                     []model.CatalogEntry
	plan                        model.CreationPlan
	writes                      int
	readErr, lateErr, sourceErr error
}

func (memory *creationMemory) Catalog(
	context.Context,
) ([]model.CatalogEntry, error) {
	return memory.entries, memory.readErr
}

func (memory *creationMemory) Select(
	context.Context, string, string,
) (model.RootSelection, error) {
	return model.RootSelection{
		ID: "root", Label: "Source", Digest: "root-digest",
	}, memory.sourceErr
}

func (memory *creationMemory) CommitCreate(
	_ context.Context, plan model.CreationPlan,
) (model.Summary, error) {
	if memory.lateErr != nil {
		memory.writes++
		memory.plan = plan
		return model.Summary{}, memory.lateErr
	}
	memory.writes++
	memory.plan = plan
	return model.Summary{
		ID: plan.ImportID, State: "QUEUED", Version: 1,
	}, nil
}

func creationFixture() (*Creation, *creationMemory) {
	size := int64(4)
	memory := &creationMemory{entries: []model.CatalogEntry{
		{Item: model.CatalogItem{
			RequirementID:      "b",
			RequirementVersion: 2,
			SourceKind:         "STATIC",
			ExpectedSize:       &size,
		}},
		{Item: model.CatalogItem{
			RequirementID:      "a",
			RequirementVersion: 3,
			SourceKind:         "STATIC",
			ExpectedSize:       &size,
		}},
	}}
	return NewCreation(
		memory, memory, func() time.Time { return time.UnixMilli(123) },
	), memory
}

func TestCreateFreezesOrderedCatalogAndExecutionInput(t *testing.T) {
	service, memory := creationFixture()
	request := model.CreateRequest{
		Kind: "BIOS_DIRECTORY", RootID: "root",
		SourceRelativePath: "nested", ReplaceIfBetter: true,
	}
	result, err := service.Create(t.Context(), request, "actor")
	if err != nil {
		t.Fatal(err)
	}
	plan := memory.plan
	if result.ID == "" ||
		plan.Items[0].RequirementID != "a" ||
		memory.entries[0].Item.RequirementID != "b" {
		t.Fatalf("catalog ordering or result: %+v", plan)
	}
	encoded, err := model.CanonicalCatalogJSON(plan.Items)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	inputDigest := sha256.Sum256(plan.Input)
	if plan.CatalogDigest != hex.EncodeToString(digest[:]) ||
		plan.InputDigest != hex.EncodeToString(inputDigest[:]) {
		t.Fatal("snapshot digests do not describe frozen bytes")
	}
	var input struct {
		ExecutionID string
		Inputs      struct {
			ServerImportVersion                     int64
			RootConfigDigest, CatalogSnapshotDigest string
			SourceRelativePath                      string
			ReplaceIfBetter                         bool
		}
	}
	if err := json.Unmarshal(plan.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.ExecutionID == "" ||
		input.Inputs.ServerImportVersion != 1 ||
		input.Inputs.RootConfigDigest != "root-digest" ||
		input.Inputs.CatalogSnapshotDigest != plan.CatalogDigest ||
		input.Inputs.SourceRelativePath != "nested" ||
		!input.Inputs.ReplaceIfBetter {
		t.Fatalf("frozen input: %+v", input)
	}
}

func TestCreateRejectsUnusableSourcesAndCatalogBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*creationMemory)
		want   error
	}{
		{"source unavailable", func(m *creationMemory) {
			m.sourceErr = context.Canceled
		}, context.Canceled},
		{"storage unavailable", func(m *creationMemory) {
			m.readErr = context.DeadlineExceeded
		}, context.DeadlineExceeded},
		{"empty", func(m *creationMemory) {
			m.entries = nil
		}, model.ErrCatalogEmpty},
		{"no expectation", func(m *creationMemory) {
			m.entries[0].Item.ExpectedSize = nil
		}, model.ErrCatalogInvalid},
		{"DAT not ready", func(m *creationMemory) {
			m.entries[0].Item.SourceKind = "DAT_MACHINE"
		}, model.ErrCatalogInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, memory := creationFixture()
			test.change(memory)
			result, err := service.Create(
				t.Context(),
				model.CreateRequest{
					Kind: "BIOS_DIRECTORY", RootID: "root",
				},
				"actor",
			)
			if !errors.Is(err, test.want) ||
				memory.writes != 0 || result.ID != "" {
				t.Fatalf(
					"rejected creation: %+v %v writes=%d",
					result, err, memory.writes,
				)
			}
		})
	}
}

func TestCreateDoesNotPublishSummaryWhenCommitFails(t *testing.T) {
	service, memory := creationFixture()
	memory.lateErr = context.Canceled
	result, err := service.Create(
		t.Context(),
		model.CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "root"},
		"actor",
	)
	if !errors.Is(err, context.Canceled) ||
		result.ID != "" || memory.writes != 1 {
		t.Fatalf("commit failure: %+v %v", result, err)
	}
}
