package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	library "retrom/internal/service/libraryimport"
)

var ErrExecutionObservation = errors.New("EmulationStation execution observation failed")

type CompanionOwner struct {
	Before         OwnedItem
	Mapping        MappingTarget
	CollectionID   string
	MappingVersion int64
}
type CompanionFile struct {
	ItemID, CollectionID, Path, Facts string
	Ordinal, Size                     int64
}
type CompanionSelection struct {
	Owner CompanionOwner
	Files []CompanionFile
}
type CompanionBinding struct {
	Before CompanionOwner
	File   CompanionFile
	Blob   VerifiedBlob
	NowMS  int64
}
type CompanionReader interface {
	Owner(context.Context, string) (CompanionOwner, error)
	Target(context.Context, string) (MappingTarget, bool, error)
	Dependencies(context.Context, string, string) ([]string, error)
	Candidates(context.Context, CompanionOwner) ([]CompanionFile, error)
}
type CompanionWriter interface {
	Register(context.Context, CompanionBinding) (string, error)
}
type CompanionScope struct {
	Read  CompanionReader
	Write CompanionWriter
}
type CompanionRepository interface {
	WithCompanions(context.Context, func(CompanionScope) error) error
}
type CompanionSources interface {
	CopyFile(context.Context, Execution, ExecutionFile) (VerifiedBlob, error)
}
type Companions struct {
	repository CompanionRepository
	sources    CompanionSources
	now        func() time.Time
}

func NewCompanions(repository CompanionRepository, sources CompanionSources, now func() time.Time) *Companions {
	return &Companions{repository: repository, sources: sources, now: now}
}

func (service *Companions) Files(
	ctx context.Context,
	unit Execution,
	item ExecutionItem,
) ([]library.ServerSourceFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("stop EmulationStation companion preparation: %w", err)
	}
	if !arcadeCompanionItem(item) {
		return []library.ServerSourceFile{}, nil
	}
	selection, err := service.Find(ctx, unit, item.ID)
	if err != nil {
		return nil, err
	}
	result := make([]library.ServerSourceFile, 0, len(selection.Files))
	for _, file := range selection.Files {
		source := ExecutionFile{Ordinal: file.Ordinal, Path: file.Path, Facts: file.Facts, Size: file.Size}
		blob, err := service.sources.CopyFile(ctx, unit, source)
		if err != nil {
			if stop := importStopCause(ctx, err); stop != nil {
				return nil, stop
			}
			if errors.Is(err, ErrExecutionObservation) {
				return nil, fmt.Errorf("observe EmulationStation companion copy: %w", err)
			}
			continue
		}
		id, err := service.Record(ctx, unit, selection.Owner, file, blob)
		if err != nil {
			return nil, err
		}
		result = append(result, library.ServerSourceFile{RelativePath: file.Path, BlobID: id, SizeBytes: file.Size})
	}
	return result, nil
}

func (service *Companions) Find(ctx context.Context, unit Execution, itemID string) (CompanionSelection, error) {
	var result CompanionSelection
	err := service.repository.WithCompanions(ctx, func(scope CompanionScope) error {
		var err error
		result.Owner, result.Files, err = service.load(ctx, scope.Read, unit, itemID, service.now().UnixMilli())
		return err
	})
	if err != nil {
		return CompanionSelection{}, fmt.Errorf("find EmulationStation companions: %w", err)
	}
	return result, nil
}

func (service *Companions) Record(
	ctx context.Context,
	unit Execution,
	selected CompanionOwner,
	file CompanionFile,
	blob VerifiedBlob,
) (string, error) {
	if selected.Before.Execution.Execution != unit {
		return "", ErrVersionConflict
	}
	if blob.SHA256 == "" || blob.Size < 0 || blob.Size != file.Size {
		return "", ErrInvalid
	}
	var id string
	err := service.repository.WithCompanions(ctx, func(scope CompanionScope) error {
		now := service.now().UnixMilli()
		owner, candidates, err := service.load(ctx, scope.Read, unit, selected.Before.Item.ID, now)
		if err != nil {
			return err
		}
		if !sameCompanionOwner(owner, selected) || !slices.Contains(candidates, file) {
			return ErrVersionConflict
		}
		id, err = scope.Write.Register(ctx, CompanionBinding{Before: owner, File: file, Blob: blob, NowMS: now})
		if err != nil {
			return fmt.Errorf("register EmulationStation companion: %w", err)
		}
		if id == "" {
			return ErrInvalid
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("record EmulationStation companion: %w", err)
	}
	return id, nil
}

func arcadeCompanionItem(item ExecutionItem) bool {
	return item.TargetPlatformKind == "arcade" && len(item.Files) == 1 &&
		strings.EqualFold(path.Ext(item.Files[0].Path), ".zip") && item.TargetDATVersionID != ""
}
