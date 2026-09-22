package sourceimport

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

type (
	CompanionCandidate struct {
		ItemID string
		File   ExecutionFile
	}
	CompanionRegistration struct {
		Before    OwnedItem
		Candidate CompanionCandidate
		Blob      VerifiedBlob
		NowMS     int64
	}
	CompanionReader interface {
		Owner(context.Context, string) (OwnedItem, error)
		Dependencies(context.Context, string, string) ([]string, error)
		Candidates(context.Context, ExecutionItem) ([]CompanionCandidate, error)
	}
	CompanionWriter interface {
		Register(context.Context, CompanionRegistration) (string, error)
	}
	CompanionScope struct {
		Read  CompanionReader
		Write CompanionWriter
	}
	CompanionRepository interface {
		WithCompanions(context.Context, func(CompanionScope) error) error
	}
	Companions struct {
		repository CompanionRepository
		now        func() time.Time
	}
)

func NewCompanions(repository CompanionRepository, now func() time.Time) *Companions {
	return &Companions{repository: repository, now: now}
}

func (service *Companions) Find(
	ctx context.Context,
	id ExecutionIdentity,
	itemID string,
) ([]CompanionCandidate, error) {
	var result []CompanionCandidate
	err := service.repository.WithCompanions(ctx, func(scope CompanionScope) error {
		before, _, err := service.owner(ctx, scope.Read, id, itemID)
		if err != nil {
			return err
		}
		result, err = companionCandidates(ctx, scope.Read, before.Item)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("find Source Arcade companions: %w", err)
	}
	return result, nil
}

func (service *Companions) Record(
	ctx context.Context,
	id ExecutionIdentity,
	itemID string,
	candidate CompanionCandidate,
	blob VerifiedBlob,
) (string, error) {
	if blob.Size != candidate.File.Size || blob.Size < 0 || blob.SHA256 == "" {
		return "", ErrInvalid
	}
	var result string
	err := service.repository.WithCompanions(ctx, func(scope CompanionScope) error {
		before, now, err := service.owner(ctx, scope.Read, id, itemID)
		if err != nil {
			return err
		}
		candidates, err := companionCandidates(ctx, scope.Read, before.Item)
		if err != nil {
			return err
		}
		found := false
		for _, current := range candidates {
			if current == candidate {
				found = true
				break
			}
		}
		if !found {
			return ErrVersionConflict
		}
		result, err = scope.Write.Register(ctx, CompanionRegistration{
			Before: before, Candidate: candidate, Blob: blob, NowMS: now,
		})
		if err != nil {
			return fmt.Errorf("register companion: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("record Source Arcade companion: %w", err)
	}
	return result, nil
}

func (service *Companions) owner(
	ctx context.Context,
	reader CompanionReader,
	id ExecutionIdentity,
	itemID string,
) (OwnedItem, int64, error) {
	before, err := reader.Owner(ctx, itemID)
	if err != nil {
		return OwnedItem{}, 0, fmt.Errorf("read companion owner: %w", err)
	}
	now := service.now().UnixMilli()
	if err := ValidateExecution(before.Execution, id, now); err != nil {
		return OwnedItem{}, 0, err
	}
	if before.Item.ID != itemID || before.Item.ImportID != id.ImportID || before.Item.State != "COPYING" ||
		before.Execution.JobState != "RUNNING" || before.Execution.Kind != "IMPORT_RECEIVE" ||
		!validItemVersion(before.Item.Version) {
		return OwnedItem{}, 0, ErrVersionConflict
	}
	return before, now, nil
}

func companionCandidates(
	ctx context.Context,
	reader CompanionReader,
	item ExecutionItem,
) ([]CompanionCandidate, error) {
	result := []CompanionCandidate{}
	if item.TargetDATVersionID == "" || len(item.Files) != 1 || !strings.EqualFold(path.Ext(item.Files[0].Path), ".zip") {
		return result, nil
	}
	machine := strings.TrimSuffix(path.Base(item.Files[0].Path), path.Ext(item.Files[0].Path))
	dependencies, err := reader.Dependencies(ctx, item.TargetDATVersionID, machine)
	if err != nil {
		return nil, fmt.Errorf("read companion dependency closure: %w", err)
	}
	if len(dependencies) == 0 {
		return result, nil
	}
	required := make(map[string]bool, len(dependencies))
	for _, dependency := range dependencies {
		required[dependency] = true
	}
	candidates, err := reader.Candidates(ctx, item)
	if err != nil {
		return nil, fmt.Errorf("read companion candidates: %w", err)
	}
	for _, candidate := range candidates {
		file := candidate.File
		candidateMachine := strings.TrimSuffix(path.Base(file.Path), path.Ext(file.Path))
		if strings.EqualFold(path.Ext(file.Path), ".zip") && required[candidateMachine] {
			result = append(result, candidate)
		}
	}
	return result, nil
}
