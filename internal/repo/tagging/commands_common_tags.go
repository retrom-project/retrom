package tagging

import (
	"context"
	"fmt"

	"retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
)

func (repository *Repository) CommitEnsureCommonTags(
	ctx context.Context, cmd tagging.EnsureCommonTagsCommand,
) (tagging.CommonTagsResult, error) {
	if err := tagging.ValidateEnsureCommonTagsCommand(cmd); err != nil {
		return tagging.CommonTagsResult{}, fmt.Errorf("tagging: validate command: %w", err)
	}
	result := tagging.CommonTagsResult{
		CreatedItems:  []tagging.AdminItem{},
		ExistingItems: []tagging.AdminItem{},
	}
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		records := tagRecords{exec}

		activeByKey, err := records.ActiveByNameKey(ctx)
		if err != nil {
			return fmt.Errorf("tagging: read active tags: %w", err)
		}

		missingCount := countMissing(cmd.Candidates, activeByKey)
		if err := tagging.ValidateEnsureCommonAdmission(len(activeByKey), missingCount); err != nil {
			return fmt.Errorf("tagging: validate common tags: %w", err)
		}

		var applyErr error
		result, applyErr = applyCommonCandidates(
			ctx, exec, records, cmd, activeByKey,
		)
		return applyErr
	})
	if err != nil {
		return result, fmt.Errorf("tagging: commit ensure common tags: %w", err)
	}
	return result, nil
}

func countMissing(candidates []tagging.CommonTagCandidate, activeByKey map[string]string) int {
	n := 0
	for _, c := range candidates {
		if activeByKey[c.NameKey] == "" {
			n++
		}
	}
	return n
}

func applyCommonCandidates(
	ctx context.Context,
	exec dbexec.Executor,
	records tagRecords,
	cmd tagging.EnsureCommonTagsCommand,
	activeByKey map[string]string,
) (tagging.CommonTagsResult, error) {
	result := tagging.CommonTagsResult{
		CreatedItems:  []tagging.AdminItem{},
		ExistingItems: []tagging.AdminItem{},
	}
	for _, candidate := range cmd.Candidates {
		if existingID := activeByKey[candidate.NameKey]; existingID != "" {
			existing, err := records.Get(ctx, existingID)
			if err != nil {
				return result, fmt.Errorf("tagging: read existing common tag: %w", err)
			}
			result.ExistingItems = append(result.ExistingItems, existing)
			continue
		}
		if err := records.Insert(ctx, tagging.TagWrite{
			ID: candidate.TagID, Name: candidate.Name, NameKey: candidate.NameKey,
			SearchText: candidate.SearchText, ActorUserID: cmd.ActorUserID, NowMS: cmd.NowMS,
		}); err != nil {
			return result, fmt.Errorf("tagging: create common tag: %w", err)
		}
		created, err := records.Get(ctx, candidate.TagID)
		if err != nil {
			return result, fmt.Errorf("tagging: read created common tag: %w", err)
		}
		result.CreatedItems = append(result.CreatedItems, created)

		if err := (auditRecords{exec}).Record(ctx, buildAuditEvent(
			candidate.AuditID, cmd.ActorUserID, "TAG_CREATED", "TAG",
			candidate.TagID, nil, created, nil, cmd.NowMS,
		)); err != nil {
			return result, fmt.Errorf("tagging: write common tag audit: %w", err)
		}
	}
	return result, nil
}
