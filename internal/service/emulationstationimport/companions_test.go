package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"

	library "retrom/internal/model/libraryimport"
)

type companionMemory struct {
	owner                                 model.CompanionOwner
	candidates                            []model.CompanionFile
	dependencies                          []string
	target                                model.MappingTarget
	beforeRegister                        func()
	readErr, copyErr, writeErr, commitErr error
	copies, writes                        int
}

func (memory *companionMemory) WithCompanions(_ context.Context, run func(model.CompanionScope) error) error {
	if err := run(model.CompanionScope{Read: memory, Write: memory}); err != nil {
		return err
	}
	return memory.commitErr
}

func (memory *companionMemory) Owner(context.Context, string) (model.CompanionOwner, error) {
	return memory.owner, memory.readErr
}

func (memory *companionMemory) Target(context.Context, string) (model.MappingTarget, bool, error) {
	return memory.target, true, memory.readErr
}

func (memory *companionMemory) Dependencies(context.Context, string, string) ([]string, error) {
	return memory.dependencies, memory.readErr
}

func (memory *companionMemory) Candidates(context.Context, model.CompanionOwner) ([]model.CompanionFile, error) {
	return memory.candidates, memory.readErr
}

func (memory *companionMemory) Register(_ context.Context, _ model.CompanionBinding) (string, error) {
	memory.writes++
	return "blob", memory.writeErr
}

func (memory *companionMemory) CopyFile(context.Context, model.Execution, model.ExecutionFile) (model.VerifiedBlob, error) {
	memory.copies++
	if memory.beforeRegister != nil {
		memory.beforeRegister()
	}
	return model.VerifiedBlob{SHA256: "hash", Size: 2}, memory.copyErr
}

func newCompanionMemory() *companionMemory {
	owned := newItemWorkMemory().before
	owned.Item.State = "COPYING"
	owned.Item.TargetPlatformKind = "arcade"
	owned.Item.TargetPlatformID = "target"
	owned.Item.TargetDATVersionID = "dat"
	owned.Item.Files = []model.ExecutionFile{{Path: "child.zip", Facts: "facts", Size: 1}}
	dat := "dat"
	target := model.MappingTarget{
		InstanceID:      "target",
		InstanceVersion: 1,
		PlatformID:      "arcade",
		CoreID:          "core",
		ProviderID:      "provider",
		TargetID:        "runtime",
		DATVersionID:    &dat,
	}
	return &companionMemory{
		owner:        model.CompanionOwner{Before: owned, Mapping: target, CollectionID: "collection", MappingVersion: 1},
		target:       target,
		dependencies: []string{"parent"},
		candidates: []model.CompanionFile{
			{ItemID: "parent-source", CollectionID: "collection", Ordinal: 0, Path: "parent.zip", Facts: "frozen", Size: 2},
			{ItemID: "other", Path: "unrelated.zip", Size: 2},
		},
	}
}

func (memory *companionMemory) service() *Companions {
	return NewCompanions(memory, memory, func() time.Time { return time.UnixMilli(2000) })
}

func TestCompanionsSelectOnlyFrozenDependencyAndFenceEachRegistration(t *testing.T) {
	memory := newCompanionMemory()
	files, err := memory.service().Files(t.Context(), memory.owner.Before.Execution.Execution, memory.owner.Before.Item)
	if err != nil || len(

		files,
	) != 1 || files[0].RelativePath != "parent.zip" || files[0].BlobID != "blob" || memory.copies != 1 || memory.writes != 1 {
		t.Fatalf("files=%#v error=%v copies=%d writes=%d", files, err, memory.copies, memory.writes)
	}
}

func TestCompanionsRejectChangedOwnershipAndCandidateBeforeRegister(t *testing.T) {
	for _, kind := range []string{"worker", "lease", "target", "candidate", "primary version", "mapping version"} {
		t.Run(kind, func(t *testing.T) {
			memory := newCompanionMemory()
			unit := memory.owner.Before.Execution.Execution
			memory.beforeRegister = func() {
				switch kind {
				case "worker":
					memory.owner.Before.Execution.WorkerID = "replacement"
				case "lease":
					memory.owner.Before.Execution.LeaseUntilMS = 2000
				case "target":
					memory.target.InstanceVersion++
				case "candidate":
					memory.candidates[0].Facts = "replaced"
				case "primary version":
					memory.owner.Before.Item.Version++
				case "mapping version":
					memory.owner.MappingVersion++
				}
			}
			files, err := memory.service().Files(t.Context(), unit, memory.owner.Before.Item)
			if !errors.Is(err, model.ErrVersionConflict) || files != nil || memory.writes != 0 {
				t.Fatalf("files=%#v error=%v writes=%d", files, err, memory.writes)
			}
		})
	}
}

func TestCompanionsKeepOptionalReadPolicyButReturnStorageAndStopCauses(t *testing.T) {
	cause := errors.New("storage error")
	for _, kind := range []string{"missing", "observation", "write", "read", "commit"} {
		t.Run(kind, func(t *testing.T) {
			memory := newCompanionMemory()
			expected := cause
			switch kind {
			case "missing":
				memory.copyErr = cause
				expected = nil
			case "observation":
				memory.copyErr = errors.Join(ErrExecutionObservation, cause)
			case "write":
				memory.writeErr = cause
			case "read":
				memory.readErr = cause
			case "commit":
				memory.commitErr = cause
			}
			files, err := memory.service().Files(t.Context(), memory.owner.Before.Execution.Execution, memory.owner.Before.Item)
			if expected == nil {
				if err != nil || !reflect.DeepEqual(files, []library.ServerSourceFile{}) {
					t.Fatalf("optional result=%#v error=%v", files, err)
				}
				return
			}
			if !errors.Is(err, expected) || files != nil {
				t.Fatalf("files=%#v error=%v", files, err)
			}
		})
	}
}
