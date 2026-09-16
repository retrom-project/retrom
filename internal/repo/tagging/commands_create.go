package tagging

import (
	"context"
	"fmt"

	"retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
)

func (repository *Repository) CommitCreate(ctx context.Context, cmd tagging.CreateCommand) (tagging.AdminItem, error) {
	var result tagging.AdminItem
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		records := tagRecords{exec}

		activeByKey, err := records.ActiveByNameKey(ctx)
		if err != nil {
			return fmt.Errorf("tagging: read active tags: %w", err)
		}
		if err := tagging.ValidateCreateAdmission(activeByKey, cmd.NameKey); err != nil {
			return fmt.Errorf("tagging: validate create: %w", err)
		}

		if err := records.Insert(ctx, tagging.TagWrite{
			ID: cmd.TagID, Name: cmd.Name, NameKey: cmd.NameKey,
			SearchText: cmd.SearchText, ActorUserID: cmd.ActorUserID, NowMS: cmd.NowMS,
		}); err != nil {
			return fmt.Errorf("tagging: insert tag: %w", err)
		}

		result, err = records.Get(ctx, cmd.TagID)
		if err != nil {
			return fmt.Errorf("tagging: read created tag: %w", err)
		}

		return (auditRecords{exec}).Record(ctx, buildCreateAudit(cmd, result))
	})
	if err != nil {
		return result, fmt.Errorf("tagging: commit create: %w", err)
	}
	return result, nil
}

func buildCreateAudit(cmd tagging.CreateCommand, result tagging.AdminItem) tagging.AuditEvent {
	return buildAuditEvent(cmd.AuditID, cmd.ActorUserID, "TAG_CREATED", "TAG", cmd.TagID, nil, result, nil, cmd.NowMS)
}
