package tagging

import (
	"context"
	"fmt"

	"retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
)

func (repository *Repository) CommitRename(ctx context.Context, cmd tagging.RenameCommand) (tagging.AdminItem, error) {
	if err := tagging.ValidateRenameCommand(cmd); err != nil {
		return tagging.AdminItem{}, fmt.Errorf("tagging: validate command: %w", err)
	}
	var result tagging.AdminItem
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		records := tagRecords{exec}

		before, err := records.Get(ctx, cmd.TagID)
		if err != nil {
			return fmt.Errorf("tagging: read tag: %w", err)
		}

		activeByKey, err := records.ActiveByNameKey(ctx)
		if err != nil {
			return fmt.Errorf("tagging: read active tags: %w", err)
		}

		if err := tagging.ValidateRenameAdmission(
			before, cmd.ExpectedVersion, cmd.Name,
			activeByKey, cmd.NameKey,
		); err != nil {
			return fmt.Errorf("tagging: validate rename: %w", err)
		}

		if err := records.Rename(ctx, tagging.TagWrite{
			ID: cmd.TagID, Name: cmd.Name, NameKey: cmd.NameKey,
			SearchText: cmd.SearchText, ActorUserID: cmd.ActorUserID,
			ExpectedVersion: cmd.ExpectedVersion, NowMS: cmd.NowMS,
		}); err != nil {
			return fmt.Errorf("tagging: rename tag: %w", err)
		}

		result, err = records.Get(ctx, cmd.TagID)
		if err != nil {
			return fmt.Errorf("tagging: read renamed tag: %w", err)
		}

		return (auditRecords{exec}).Record(ctx, buildAuditEvent(
			cmd.AuditID, cmd.ActorUserID, "TAG_RENAMED", "TAG", cmd.TagID,
			before, result,
			map[string]any{
				"name": map[string]string{
					"before": before.Name,
					"after":  result.Name,
				},
			},
			cmd.NowMS,
		))
	})
	if err != nil {
		return result, fmt.Errorf("tagging: commit rename: %w", err)
	}
	return result, nil
}
