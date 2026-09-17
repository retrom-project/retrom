package gamecontent

import (
	"context"
	"fmt"

	model "retrom/internal/model/gamecontent"
)

func (service *Service) publish(
	ctx context.Context,
	claim model.Claim,
	snapshot model.JobSnapshot,
	prepared model.PreparedReplacement,
) error {
	now := service.now().UnixMilli()
	result, err := service.repository.CommitPublish(ctx, model.PublishCommand{
		Claim: claim, Snapshot: snapshot, Prepared: prepared, NowMS: now,
	})
	if err != nil {
		return fmt.Errorf("commit replacement publication: %w", err)
	}
	if result.SignalRelease && service.payloadReleases != nil {
		service.payloadReleases.Signal()
	}
	return nil
}
