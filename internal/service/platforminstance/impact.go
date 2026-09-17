package platforminstance

import (
	"context"
	"fmt"

	model "retrom/internal/model/platforminstance"

	"github.com/google/uuid"
)

func (service *Service) CoreImpact(
	ctx context.Context, instanceID, coreID string, expected int64,
) (model.CoreImpactResult, error) {
	if instanceID == "" || coreID == "" || expected < 1 {
		return model.CoreImpactResult{}, model.ErrInvalid
	}
	facts, err := service.repository.LoadCoreImpactFacts(ctx, instanceID, coreID, expected)
	if err != nil {
		return model.CoreImpactResult{}, repositoryError("core impact", err)
	}
	return model.ProjectCoreImpact(instanceID, coreID, facts), nil
}

func (service *Service) ChangeDefaultCore(
	ctx context.Context,
	instanceID, coreID string,
	expected int64,
	digest string,
	confirmBlocked bool,
	actor model.AuditActor,
) (model.DefaultCoreChangeResult, error) {
	if instanceID == "" || coreID == "" || expected < 1 || digest == "" {
		return model.DefaultCoreChangeResult{}, model.ErrImpactStale
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return model.DefaultCoreChangeResult{}, fmt.Errorf("platforminstance: create audit id: %w", err)
	}
	change, err := service.repository.CommitChangeDefaultCore(ctx, model.ChangeDefaultCoreCommand{
		InstanceID: instanceID, CoreID: coreID, Expected: expected,
		Digest: digest, ConfirmBlocked: confirmBlocked,
		Actor: actor, NowMS: service.now().UnixMilli(), AuditID: auditID.String(),
	})
	return change, repositoryError("change default core", err)
}

