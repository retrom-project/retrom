package payloadrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	model "retrom/internal/model/payloadrelease"
)

type GarbageCollector struct {
	repository model.GarbageRepository
	authority  model.EffectAuthority
	files      model.GarbageFiles
}

func NewGarbageCollector(
	repository model.GarbageRepository, authority model.EffectAuthority, files model.GarbageFiles,
) *GarbageCollector {
	return &GarbageCollector{repository: repository, authority: authority, files: files}
}

func (service *GarbageCollector) Execute(ctx context.Context, unit model.Execution) error {
	if !validGarbageInput(unit) {
		return effectFailure("BLOB_GC_INPUT_INVALID", nil)
	}
	workFence, err := service.validateGarbageAuthority(ctx, unit.Work)
	if err != nil {
		return fmt.Errorf("check garbage execution: %w", err)
	}
	facts, err := service.repository.LoadGarbageFacts(
		ctx, unit.Work.Scope.ID, unit.Input.Inputs.SHA256,
	)
	if err != nil {
		return fmt.Errorf("read garbage ownership: %w", err)
	}
	cmd, physicalRemove := service.garbageCommand(workFence, unit, facts)
	if err := service.repository.CommitGarbage(
		ctx, cmd, service.authority,
	); err != nil {
		return fmt.Errorf("commit garbage catalog removal: %w", err)
	}
	if physicalRemove {
		if err := service.files.Delete(ctx, unit.Input.Inputs.SHA256); err != nil {
			return effectFailure("BLOB_GC_PHYSICAL_DELETE_FAILED", err)
		}
	}
	return nil
}

func (service *GarbageCollector) validateGarbageAuthority(
	ctx context.Context, unit model.Work,
) (model.Work, error) {
	before, found, err := service.repository.LoadGarbageWork(ctx, unit.ID)
	if err != nil {
		return model.Work{}, fmt.Errorf("read garbage authority: %w", err)
	}
	if !found || before.State != "RUNNING" || before.WorkerID == "" ||
		before.WorkerID != unit.WorkerID || before.ExecutionNo != unit.ExecutionNo {
		return model.Work{}, model.ErrExecutionLost
	}
	return before, nil
}

func (service *GarbageCollector) garbageCommand(
	workFence model.Work, unit model.Execution, facts model.GarbageFacts,
) (model.GarbageCommand, bool) {
	cmd := model.GarbageCommand{WorkFence: workFence, Facts: facts}
	if facts.OtherDigestOwner || !facts.Found {
		return cmd, !facts.OtherDigestOwner && !facts.Found
	}
	blob := facts.Blob
	if blob.ID != unit.Work.Scope.ID ||
		blob.Digest != unit.Input.Inputs.SHA256 {
		return cmd, false
	}
	if !blob.HasCandidate || blob.Candidate.Work.ID != unit.Work.ID {
		return cmd, false
	}
	if blob.Protected {
		cmd.Cancel = true
		return cmd, false
	}
	cmd.Remove = true
	return cmd, true
}

func validGarbageInput(unit model.Execution) bool {
	digest, err := hex.DecodeString(unit.Input.Inputs.SHA256)
	return err == nil && len(digest) == sha256.Size &&
		unit.Work.Scope.Type == model.ScopeBlob && unit.Work.Scope.ID != "" &&
		unit.Input.Scope == unit.Work.Scope &&
		unit.Input.Kind == "BLOB_GC" &&
		unit.Input.SchemaVersion == 1
}

type effectError struct {
	code  string
	cause error
}

func (err effectError) Error() string {
	if err.cause == nil {
		return err.code
	}
	return fmt.Sprintf("%s: %v", err.code, err.cause)
}
func (err effectError) Code() string               { return err.code }
func (err effectError) Unwrap() error              { return err.cause }
func effectFailure(code string, cause error) error { return effectError{code: code, cause: cause} }
