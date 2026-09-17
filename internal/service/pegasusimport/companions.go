package pegasusimport

import (
	"context"
	"fmt"
	"path"
	model "retrom/internal/model/pegasusimport"
	"strings"
	"time"
)

type Companions struct {
	repository model.CompanionRepository
	now        func() time.Time
}

func NewCompanions(repository model.CompanionRepository, now func() time.Time) *Companions {
	return &Companions{repository: repository, now: now}
}

func (service *Companions) Find(
	ctx context.Context,
	id model.ExecutionIdentity,
	itemID string,
) ([]model.CompanionCandidate, error) {
	var result []model.CompanionCandidate
	err := service.repository.WithCompanions(ctx, func(scope model.CompanionScope) error {
		before, _, err := service.owner(ctx, scope.Read, id, itemID)
		if err != nil {
			return err
		}
		result, err = companionCandidates(ctx, scope.Read, before.Item)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("find Pegasus Arcade companions: %w", err)
	}
	return result, nil
}

func (service *Companions) Record(
	ctx context.Context,
	id model.ExecutionIdentity,
	itemID string,
	candidate model.CompanionCandidate,
	blob model.VerifiedBlob,
) (string, error) {
	if blob.Size != candidate.File.Size || blob.Size < 0 || blob.SHA256 == "" {
		return "", model.ErrInvalid
	}
	var result string
	err := service.repository.WithCompanions(ctx, func(scope model.CompanionScope) error {
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
			return model.ErrVersionConflict
		}
		result, err = scope.Write.Register(ctx, model.CompanionRegistration{
			Before: before, Candidate: candidate, Blob: blob, NowMS: now,
		})
		if err != nil {
			return fmt.Errorf("register companion: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("record Pegasus Arcade companion: %w", err)
	}
	return result, nil
}

func (service *Companions) owner(
	ctx context.Context,
	reader model.CompanionReader,
	id model.ExecutionIdentity,
	itemID string,
) (model.OwnedItem, int64, error) {
	before, err := reader.Owner(ctx, itemID)
	if err != nil {
		return model.OwnedItem{}, 0, fmt.Errorf("read companion owner: %w", err)
	}
	now := service.now().UnixMilli()
	if err := ValidateExecution(before.Execution, id, now); err != nil {
		return model.OwnedItem{}, 0, err
	}
	if before.Item.ID != itemID || before.Item.ImportID != id.ImportID || before.Item.State != "COPYING" ||
		before.Execution.JobState != "RUNNING" || before.Execution.Kind != "SERVER_PEGASUS_IMPORT" ||
		!validItemVersion(before.Item.Version) {
		return model.OwnedItem{}, 0, model.ErrVersionConflict
	}
	return before, now, nil
}

func companionCandidates(
	ctx context.Context,
	reader model.CompanionReader,
	item model.ExecutionItem,
) ([]model.CompanionCandidate, error) {
	result := []model.CompanionCandidate{}
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
