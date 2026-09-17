package tagging

import (
	"context"
	"fmt"

	"retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
)

func (repository *Repository) CommitDelete(
	ctx context.Context, cmd tagging.DeleteCommand,
) (tagging.AdminItem, tagging.DeleteImpact, error) {
	if err := tagging.ValidateDeleteCommand(cmd); err != nil {
		return tagging.AdminItem{}, tagging.DeleteImpact{}, fmt.Errorf("tagging: validate command: %w", err)
	}
	var result tagging.AdminItem
	var impact tagging.DeleteImpact
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		records := tagRecords{exec}

		before, err := records.Get(ctx, cmd.TagID)
		if err != nil {
			return fmt.Errorf("tagging: read tag: %w", err)
		}
		if err := tagging.ValidateDeleteAdmission(before, cmd.ExpectedVersion, cmd.ConfirmName); err != nil {
			return fmt.Errorf("tagging: validate delete: %w", err)
		}

		impact = tagging.DeleteImpact(before.Usage)

		if err := records.Delete(ctx, tagging.TagWrite{
			ID: cmd.TagID, ActorUserID: cmd.ActorUserID,
			ExpectedVersion: cmd.ExpectedVersion, NowMS: cmd.NowMS,
		}); err != nil {
			return fmt.Errorf("tagging: delete tag: %w", err)
		}

		result, err = records.Get(ctx, cmd.TagID)
		if err != nil {
			return fmt.Errorf("tagging: read deleted tag: %w", err)
		}

		return (auditRecords{exec}).Record(ctx, buildAuditEvent(
			cmd.AuditID, cmd.ActorUserID, "TAG_DELETED", "TAG", cmd.TagID,
			before, result,
			map[string]any{"impact": impact},
			cmd.NowMS,
		))
	})
	if err != nil {
		return result, impact, fmt.Errorf("tagging: commit delete: %w", err)
	}
	return result, impact, nil
}
