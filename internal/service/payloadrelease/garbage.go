package payloadrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type GarbageFacts struct {
	Blob                    GCBlob
	Found, OtherDigestOwner bool
	ArchiveEntries          int64
}

type GarbageReader interface {
	Facts(context.Context, string, string) (GarbageFacts, error)
}

type GarbageWriter interface {
	Remove(context.Context, GarbageFacts) error
	Cancel(context.Context, GarbageFacts) error
}

type GarbageScope struct {
	Read   GarbageReader
	Write  GarbageWriter
	Worker WorkerScope
}

type GarbageRepository interface {
	WithGarbage(context.Context, func(GarbageScope) error) error
}

type EffectAuthority interface {
	CheckInScope(context.Context, WorkerScope, Work) error
}

type GarbageFiles interface {
	Delete(context.Context, string) error
}

type GarbageCollector struct {
	repository GarbageRepository
	authority  EffectAuthority
	files      GarbageFiles
}

func NewGarbageCollector(
	repository GarbageRepository, authority EffectAuthority, files GarbageFiles,
) *GarbageCollector {
	return &GarbageCollector{repository: repository, authority: authority, files: files}
}

func (service *GarbageCollector) Execute(ctx context.Context, unit Execution) error {
	if !validGarbageInput(unit) {
		return effectFailure("BLOB_GC_INPUT_INVALID", nil)
	}
	remove := false
	err := service.repository.WithGarbage(ctx, func(scope GarbageScope) error {
		if err := service.authority.CheckInScope(ctx, scope.Worker, unit.Work); err != nil {
			return fmt.Errorf("check garbage execution: %w", err)
		}
		facts, err := scope.Read.Facts(ctx, unit.Work.Scope.ID, unit.Input.Inputs.SHA256)
		if err != nil {
			return fmt.Errorf("read garbage ownership: %w", err)
		}
		remove, err = service.removeCatalog(ctx, scope.Write, unit, facts)
		if err != nil {
			return err
		}
		if err := service.authority.CheckInScope(ctx, scope.Worker, unit.Work); err != nil {
			return fmt.Errorf("confirm garbage execution before commit: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit garbage catalog removal: %w", err)
	}
	if remove {
		if err := service.files.Delete(ctx, unit.Input.Inputs.SHA256); err != nil {
			return effectFailure("BLOB_GC_PHYSICAL_DELETE_FAILED", err)
		}
	}
	return nil
}

func (service *GarbageCollector) removeCatalog(
	ctx context.Context, writer GarbageWriter, unit Execution, facts GarbageFacts,
) (bool, error) {
	if facts.OtherDigestOwner {
		return false, nil
	}
	if !facts.Found {
		return true, nil
	}
	blob := facts.Blob
	if blob.ID != unit.Work.Scope.ID || blob.Digest != unit.Input.Inputs.SHA256 {
		return false, effectFailure("BLOB_GC_INPUT_INVALID", nil)
	}
	if !blob.HasCandidate || blob.Candidate.Work.ID != unit.Work.ID {
		return false, nil
	}
	if blob.Protected {
		if err := writer.Cancel(ctx, facts); err != nil {
			return false, fmt.Errorf("cancel protected garbage: %w", err)
		}
		return false, nil
	}
	if err := writer.Remove(ctx, facts); err != nil {
		return false, fmt.Errorf("remove garbage catalog: %w", err)
	}
	return true, nil
}

func validGarbageInput(unit Execution) bool {
	digest, err := hex.DecodeString(unit.Input.Inputs.SHA256)
	return err == nil && len(digest) == sha256.Size && unit.Work.Scope.Type == ScopeBlob && unit.Work.Scope.ID != "" &&
		unit.Input.Scope == unit.Work.Scope && unit.Input.Kind == "BLOB_GC" && unit.Input.SchemaVersion == 1
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
