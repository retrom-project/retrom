package cleanupjobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type FileDeletionFacts struct {
	Blob           DeletionFile
	Found          bool
	ArchiveEntries int64
}

type FileDeletionReader interface {
	Facts(context.Context, string, string) (FileDeletionFacts, error)
}

type FileDeletionWriter interface {
	Remove(context.Context, FileDeletionFacts) error
}

type FileDeletionScope struct {
	Read   FileDeletionReader
	Write  FileDeletionWriter
	Worker WorkerScope
}

type FileDeletionRepository interface {
	WithFileDeletion(context.Context, func(FileDeletionScope) error) error
}

type EffectAuthority interface {
	CheckInScope(context.Context, WorkerScope, Work) error
}

type FileDeletionFiles interface {
	Delete(context.Context, string) error
}

type FileDeletionCollector struct {
	repository FileDeletionRepository
	authority  EffectAuthority
	files      FileDeletionFiles
}

func NewFileDeletionCollector(
	repository FileDeletionRepository, authority EffectAuthority, files FileDeletionFiles,
) *FileDeletionCollector {
	return &FileDeletionCollector{repository: repository, authority: authority, files: files}
}

func (service *FileDeletionCollector) Execute(ctx context.Context, unit Execution) error {
	if !validFileDeletionInput(unit) {
		return effectFailure("FILE_DELETE_INPUT_INVALID", nil)
	}
	// Deletion is irreversible once the owner retires this immutable ID.
	// Check the job lease, then perform filesystem IO outside the write transaction.
	eligible := false
	err := service.repository.WithFileDeletion(ctx, func(scope FileDeletionScope) error {
		if err := service.authority.CheckInScope(ctx, scope.Worker, unit.Work); err != nil {
			return fmt.Errorf("check file deletion authority and facts: %w", err)
		}
		facts, err := scope.Read.Facts(ctx, unit.Work.Scope.ID, unit.Input.Inputs.SHA256)
		if err != nil {
			return fmt.Errorf("check file deletion authority and facts: %w", err)
		}
		eligible = !facts.Found || validRetiredFile(facts, unit)
		if !eligible {
			return effectFailure("FILE_DELETE_OWNER_CHANGED", nil)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("authorize file deletion: %w", err)
	}
	if err := service.files.Delete(ctx, unit.Work.Scope.ID); err != nil {
		return effectFailure("FILE_DELETE_IO_FAILED", err)
	}
	err = service.repository.WithFileDeletion(ctx, func(scope FileDeletionScope) error {
		if err := service.authority.CheckInScope(ctx, scope.Worker, unit.Work); err != nil {
			return fmt.Errorf("check file deletion authority and facts: %w", err)
		}
		facts, err := scope.Read.Facts(ctx, unit.Work.Scope.ID, unit.Input.Inputs.SHA256)
		if err != nil {
			return fmt.Errorf("check file deletion authority and facts: %w", err)
		}
		if !facts.Found {
			return nil
		}
		if !validRetiredFile(facts, unit) {
			return effectFailure("FILE_DELETE_OWNER_CHANGED", nil)
		}
		if err := scope.Write.Remove(ctx, facts); err != nil {
			return fmt.Errorf("settle file deletion: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete file deletion: %w", err)
	}
	return nil
}

func validRetiredFile(facts FileDeletionFacts, unit Execution) bool {
	file := facts.Blob
	return file.ID == unit.Work.Scope.ID && file.Digest == unit.Input.Inputs.SHA256 &&
		!file.Retained && file.HasCandidate && file.Candidate.Work.ID == unit.Work.ID
}

func validFileDeletionInput(unit Execution) bool {
	digest, err := hex.DecodeString(unit.Input.Inputs.SHA256)
	return err == nil && len(digest) == sha256.Size && unit.Work.Scope.Type == ScopeFile && unit.Work.Scope.ID != "" &&
		unit.Input.Scope == unit.Work.Scope && unit.Input.Kind == "FILE_DELETE" && unit.Input.SchemaVersion == 1
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
