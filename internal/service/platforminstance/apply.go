package platforminstance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	model "retrom/internal/model/platforminstance"

	"github.com/google/uuid"

	"retrom/internal/capability/runtime/platformcatalog"
)

const applyOperationID = "postAdminPlatformInstanceRecommendationsApply"

func (service *Service) Apply(
	ctx context.Context, actor model.AuditActor, principalID, key string,
) (model.IdempotentResponse, error) {
	if principalID == "" || key == "" {
		return model.IdempotentResponse{}, model.ErrInvalid
	}
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return model.IdempotentResponse{}, fmt.Errorf("%w: %w", model.ErrCatalogInvalid, err)
	}
	digestBytes := sha256.Sum256([]byte(applyOperationID + "\x00" + principalID + "\x00{}"))
	digest := hex.EncodeToString(digestBytes[:])
	now := service.now().UnixMilli()
	instanceIDs := make([]string, len(catalog.Templates))
	auditIDs := make([]string, len(catalog.Templates))
	for i := range catalog.Templates {
		id, err := uuid.NewV7()
		if err != nil {
			return model.IdempotentResponse{}, fmt.Errorf("platforminstance: pre-generate id: %w", err)
		}
		auditID, err := uuid.NewV7()
		if err != nil {
			return model.IdempotentResponse{}, fmt.Errorf("platforminstance: pre-generate audit id: %w", err)
		}
		instanceIDs[i] = id.String()
		auditIDs[i] = auditID.String()
	}
	response, err := service.repository.CommitApply(ctx, model.ApplyCommand{
		IdempotencyKey: model.IdempotencyKey{PrincipalID: principalID, Operation: applyOperationID, Key: key},
		Digest:         digest,
		Actor:          actor,
		Catalog:        catalog,
		NowMS:          now,
		ExpiresAtMS:    now + int64(24*time.Hour/time.Millisecond),
		InstanceIDs:    instanceIDs,
		AuditIDs:       auditIDs,
	})
	return response, repositoryError("apply", err)
}
