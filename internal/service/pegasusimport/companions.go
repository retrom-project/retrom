package pegasusimport

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type Companions struct {
	repository model.CompanionRepository
	now        func() time.Time
}

func NewCompanions(
	repository model.CompanionRepository, now func() time.Time,
) *Companions {
	return &Companions{repository: repository, now: now}
}

func (service *Companions) Find(
	ctx context.Context,
	id model.ExecutionIdentity,
	itemID string,
) ([]model.CompanionCandidate, error) {
	before, _, err := service.owner(ctx, id, itemID)
	if err != nil {
		return nil, fmt.Errorf("find Pegasus Arcade companions: %w", err)
	}
	result, err := service.companionCandidates(ctx, before.Item)
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
	before, now, err := service.owner(ctx, id, itemID)
	if err != nil {
		return "", fmt.Errorf("record Pegasus Arcade companion: %w", err)
	}
	candidates, err := service.companionCandidates(ctx, before.Item)
	if err != nil {
		return "", fmt.Errorf("record Pegasus Arcade companion: %w", err)
	}
	found := false
	for _, current := range candidates {
		if current == candidate {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf(
			"record Pegasus Arcade companion: %w", model.ErrVersionConflict,
		)
	}
	result, err := service.repository.CommitCompanionRegistration(
		ctx, model.CompanionRegistration{
			Before: before, Candidate: candidate, Blob: blob, NowMS: now,
		},
	)
	if err != nil {
		return "", fmt.Errorf(
			"record Pegasus Arcade companion: register: %w", err,
		)
	}
	return result, nil
}

func (service *Companions) owner(
	ctx context.Context,
	id model.ExecutionIdentity,
	itemID string,
) (model.OwnedItem, int64, error) {
	before, err := service.repository.LoadCompanionOwner(ctx, itemID)
	if err != nil {
		return model.OwnedItem{}, 0, fmt.Errorf("read companion owner: %w", err)
	}
	now := service.now().UnixMilli()
	if err := ValidateExecution(before.Execution, id, now); err != nil {
		return model.OwnedItem{}, 0, err
	}
	if before.Item.ID != itemID ||
		before.Item.ImportID != id.ImportID ||
		before.Item.State != "COPYING" ||
		before.Execution.JobState != "RUNNING" ||
		before.Execution.Kind != "SERVER_PEGASUS_IMPORT" ||
		!validItemVersion(before.Item.Version) {
		return model.OwnedItem{}, 0, model.ErrVersionConflict
	}
	return before, now, nil
}

func (service *Companions) companionCandidates(
	ctx context.Context,
	item model.ExecutionItem,
) ([]model.CompanionCandidate, error) {
	result := []model.CompanionCandidate{}
	if item.TargetDATVersionID == "" || len(item.Files) != 1 ||
		!strings.EqualFold(path.Ext(item.Files[0].Path), ".zip") {
		return result, nil
	}
	machine := strings.TrimSuffix(
		path.Base(item.Files[0].Path), path.Ext(item.Files[0].Path),
	)
	dependencies, err := service.repository.LoadDependencies(
		ctx, item.TargetDATVersionID, machine,
	)
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
	candidates, err := service.repository.LoadCandidates(ctx, item)
	if err != nil {
		return nil, fmt.Errorf("read companion candidates: %w", err)
	}
	for _, candidate := range candidates {
		file := candidate.File
		candidateMachine := strings.TrimSuffix(
			path.Base(file.Path), path.Ext(file.Path),
		)
		if strings.EqualFold(path.Ext(file.Path), ".zip") &&
			required[candidateMachine] {
			result = append(result, candidate)
		}
	}
	return result, nil
}
