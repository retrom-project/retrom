package emulationstationimport

import (
	"context"
	"time"

	model "retrom/internal/model/emulationstationimport"

	"retrom/internal/capability/security/authn"

	"github.com/google/uuid"
)

type ReviewHandoff struct {
	repository model.ReviewHandoffRepository
	now        func() time.Time
}

func NewReviewHandoff(
	repository model.ReviewHandoffRepository,
	metadata model.ReviewMetadataSeeder,
	now func() time.Time,
) *ReviewHandoff {
	return &ReviewHandoff{repository: repository, now: now}
}

func (service *ReviewHandoff) Complete(ctx context.Context, request model.ReviewHandoffRequest) error {
	now := service.now().UnixMilli()
	auditID, _ := uuid.NewV7()
	actorKind := "SYSTEM"
	var actorUserID *string
	actorLabel := ptrString("release-setup")
	if principal, ok := authn.PrincipalFromContext(ctx); ok && principal.UserID != "" {
		actorKind = "USER"
		actorUserID = &principal.UserID
		actorLabel = nil
	}
	return service.repository.CommitReviewHandoff(ctx, request, now,
		auditID.String(), actorKind, actorUserID, actorLabel)
}

func ptrString(s string) *string { return &s }
