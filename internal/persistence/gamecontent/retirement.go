package gamecontent

import (
	"context"
	"fmt"

	"retrom/internal/payloadrelease"
	"retrom/internal/service/gamecontent"
)

func (writes writes) Retire(
	ctx context.Context,
	gameID, variantID string,
	now int64,
) (gamecontent.RetirementImpact, error) {
	if writes.releases == nil {
		return gamecontent.RetirementImpact{}, gamecontent.ErrInvalid
	}
	impact, err := writes.releases.RetireCurrentGameContent(ctx, writes.transaction, gameID, variantID, now)
	if err != nil {
		return gamecontent.RetirementImpact{}, fmt.Errorf("retire replaced game runtime: %w", err)
	}
	return gamecontent.RetirementImpact{
		SaveStateCount:   impact.SaveStateCount,
		CandidateBlobIDs: impact.CandidateBlobIDs,
	}, nil
}

func (writes writes) Stage(ctx context.Context, ids []string) error {
	if writes.releases == nil {
		return gamecontent.ErrInvalid
	}
	if err := writes.releases.StageCandidates(ctx, writes.transaction, ids); err != nil {
		return fmt.Errorf("stage replaced content: %w", err)
	}
	return nil
}

func (writes writes) ReleaseUpload(ctx context.Context, jobID string, now int64) error {
	var id string
	err := writes.transaction.QueryRowContext(ctx, `SELECT id FROM upload_consumptions
 WHERE consumer_type='GAME_CONTENT_REPLACE_JOB' AND consumer_id=?`, jobID).Scan(&id)
	if err != nil {
		return fmt.Errorf("read replacement upload consumption: %w", err)
	}
	if _, err := payloadrelease.ScheduleConsumption(ctx, writes.transaction, id, now); err != nil {
		return fmt.Errorf("release replacement upload: %w", err)
	}
	return nil
}
