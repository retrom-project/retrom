package emulationstationimport

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type materialMemory struct {
	before                       model.MaterialSnapshot
	phase                        model.ExecutionPhase
	binding                      model.MaterialBinding
	warning                      model.MaterialWarning
	phaseChange                  model.PhaseChange
	readErr, writeErr, commitErr error
}

func (memory *materialMemory) WithMaterialization(_ context.Context, run func(model.MaterialScope) error) error {
	if err := run(model.MaterialScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.commitErr
}

func (memory *materialMemory) Source(context.Context, model.MaterialKey) (model.MaterialSnapshot, error) {
	return memory.before, memory.readErr
}

func (memory *materialMemory) Execution(context.Context, string) (model.ExecutionPhase, error) {
	return memory.phase, memory.readErr
}

func (memory *materialMemory) Bind(_ context.Context, change model.MaterialBinding) (string, error) {
	memory.binding = change
	return "material-blob", memory.writeErr
}

func (memory *materialMemory) Warn(_ context.Context, change model.MaterialWarning) error {
	memory.warning = change
	return memory.writeErr
}

func (memory *materialMemory) Phase(_ context.Context, change model.PhaseChange) error {
	memory.phaseChange = change
	return memory.writeErr
}

func newMaterialMemory() *materialMemory {
	owned := newItemWorkMemory().before
	owned.Item.State = "COPYING"
	return &materialMemory{
		before: model.MaterialSnapshot{
			Before:   owned,
			Source:   model.MaterialSource{Key: model.MaterialKey{ItemID: owned.Item.ID}, Path: "game.nes", Facts: "frozen", Size: 3},
			State:    "DISCOVERED",
			Warnings: []map[string]any{},
		},
		phase: model.ExecutionPhase{Execution: owned.Execution, Phase: "COPYING_CONTENT"},
	}
}

func TestMaterializationRequiresOriginalAuthorityAndFrozenFacts(t *testing.T) {
	for _, kind := range []string{"valid", "worker", "file", "size", "state"} {
		t.Run(kind, func(t *testing.T) {
			memory := newMaterialMemory()
			unit := memory.before.Before.Execution.Execution
			source := memory.before.Source
			switch kind {
			case "worker":
				memory.before.Before.Execution.WorkerID = "other"
			case "file":
				memory.before.Source.Facts = "changed"
			case "size":
				source.Size++
			case "state":
				memory.before.Before.Item.State = "VALIDATING"
			}
			result, err := NewMaterialization(memory, func() time.Time { return time.UnixMilli(2000) }).Copy(
				t.Context(),
				unit,
				source,
				model.VerifiedBlob{SHA256: "hash", Size: source.Size},
			)
			if kind != "valid" {
				if err == nil || result != "" || memory.binding.Blob.SHA256 != "" {
					t.Fatalf("invalid binding=%#v result=%s error=%v", memory.binding, result, err)
				}
				return
			}
			if err != nil || result != "material-blob" || memory.binding.NowMS != 2000 {
				t.Fatalf("binding=%#v result=%s error=%v", memory.binding, result, err)
			}
		})
	}
}

func TestMaterializationWarningUsesESFieldsAndRetainsBound(t *testing.T) {
	memory := newMaterialMemory()
	memory.before.Source.Key.Kind = "COVER"
	for range 64 {
		memory.before.Warnings = append(memory.before.Warnings, map[string]any{"code": "PARSE_WARNING"})
	}
	service := NewMaterialization(memory, func() time.Time { return time.UnixMilli(2000) })
	err := service.Warning(
		t.Context(),
		memory.before.Before.Execution.Execution,
		memory.before.Source,
		"EMULATIONSTATION_IMAGE_INVALID",
	)
	if err != nil || memory.warning.State != "READ_FAILED" || len(
		memory.warning.Warnings,
	) != 64 || memory.warning.Warnings[63]["code"] != "WARNING_LIMIT_REACHED" {
		t.Fatalf("warning=%#v error=%v", memory.warning, err)
	}
	memory.before.Warnings = []map[string]any{}
	if err := service.Warning(
		t.Context(),
		memory.before.Before.Execution.Execution,
		memory.before.Source,
		"EMULATIONSTATION_IMAGE_INVALID",
	); err != nil {
		t.Fatal(err)
	}
	warning := memory.warning.Warnings[0]
	if warning["field"] != "image" || warning["pathKind"] != "COVER" {
		t.Fatalf("wrong ES media warning=%#v", warning)
	}
}

func TestMaterializationPreservesErrorsAndUncommittedResult(t *testing.T) {
	cause := errors.New("material database failed")
	for _, stage := range []string{"read", "write", "commit"} {
		t.Run(stage, func(t *testing.T) {
			memory := newMaterialMemory()
			switch stage {
			case "read":
				memory.readErr = cause
			case "write":
				memory.writeErr = cause
			case "commit":
				memory.commitErr = cause
			}
			result, err := NewMaterialization(memory, func() time.Time { return time.UnixMilli(2000) }).Copy(
				t.Context(),
				memory.before.Before.Execution.Execution,
				memory.before.Source,
				model.VerifiedBlob{SHA256: "hash", Size: 3},
			)
			if !errors.Is(err, cause) || result != "" {
				t.Fatalf("result=%s cause=%v", result, err)
			}
		})
	}
}
