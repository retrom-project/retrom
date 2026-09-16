package tagging

import (
	"context"
	"fmt"

	"retrom/internal/model/tagging"
	"retrom/internal/repo/dbexec"
)

func (repository *Repository) CommitReplaceGameTags(
	ctx context.Context, cmd tagging.ReplaceGameTagsCommand,
) (tagging.GameTagResult, error) {
	var result tagging.GameTagResult
	err := dbexec.Immediate(ctx, repository.database, func(exec dbexec.Executor) error {
		records := tagRecords{exec}
		games := gameRecords{exec}
		relations := relationRecords{exec}

		version, err := games.Version(ctx, cmd.GameID)
		if err != nil {
			return err
		}
		if version != cmd.ExpectedVersion {
			return tagging.ErrVersionConflict
		}

		desired, err := records.ActiveReferences(ctx, cmd.TagIDs)
		if err != nil {
			return fmt.Errorf("tagging: read active references: %w", err)
		}
		validated, err := tagging.ValidateActiveReferenceFacts(cmd.TagIDs, desired)
		if err != nil {
			return fmt.Errorf("tagging: validate references: %w", err)
		}

		owner := tagging.Owner{Kind: tagging.OwnerGame, ID: cmd.GameID}
		before, err := relations.References(ctx, owner)
		if err != nil {
			return fmt.Errorf("tagging: read game references: %w", err)
		}

		plan, err := tagging.BuildReplacementPlan(
			owner, before, validated, cmd.ActorUserID, cmd.NowMS,
		)
		if err != nil {
			return fmt.Errorf("tagging: build replacement plan: %w", err)
		}

		if !plan.Changed {
			result = tagging.GameTagResult{GameID: cmd.GameID, Version: version, Tags: before}
			return nil
		}

		if err := applyPlan(ctx, exec, plan); err != nil {
			return err
		}

		if err := games.Touch(ctx, cmd.GameID, cmd.ExpectedVersion, cmd.NowMS); err != nil {
			return fmt.Errorf("tagging: advance game version: %w", err)
		}

		added, removed := tagging.ReferenceDiff(before, validated)
		result = tagging.GameTagResult{GameID: cmd.GameID, Version: version + 1, Tags: validated}

		return (auditRecords{exec}).Record(ctx, buildAuditEvent(
			cmd.AuditID, cmd.ActorUserID, "GAME_TAGS_REPLACED", "GAME", cmd.GameID,
			before, validated,
			map[string]any{"added": added, "removed": removed},
			cmd.NowMS,
		))
	})
	if err != nil {
		return result, fmt.Errorf("tagging: commit replace game tags: %w", err)
	}
	return result, nil
}

// applyPlan delegates to the shared ApplyReplacementPlan helper.
func applyPlan(ctx context.Context, exec dbexec.Executor, plan tagging.ReplacementPlan) error {
	return ApplyReplacementPlan(ctx, exec, plan)
}
