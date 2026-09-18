package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/accounts"

	"github.com/google/uuid"
)

type LinkIssuanceService struct {
	repository model.LinkIssueRepository
	tokens     model.LinkIssuer
	now        func() time.Time
}

func NewLinkIssuance(
	repository model.LinkIssueRepository,
	tokens model.LinkIssuer,
	now func() time.Time,
) *LinkIssuanceService {
	return &LinkIssuanceService{repository, tokens, now}
}

func (service *LinkIssuanceService) Invitation(
	ctx context.Context,
	actor model.LinkCreator,
	role string,
	confirmed bool,
	key string,
) (model.AccountLink, bool, error) {
	if role != "USER" && role != "ADMIN" || (role == "ADMIN") != confirmed {
		return model.AccountLink{}, false, model.ErrRoleConfirmation
	}
	operation, err := newAccountOperation(
		"postAdminInvitation",
		actor.UserID,
		key,
		map[string]any{
			"confirmAdminRole": confirmed,
			"role":             role,
		},
		service.now().UnixMilli(),
	)
	if err != nil {
		return model.AccountLink{}, false, err
	}
	link, err := newAccountLink(actor, "INVITATION", operation.Now)
	if err != nil {
		return model.AccountLink{}, false, err
	}
	link.Role = &role
	plan := model.LinkIssuePlan{Link: link}
	return service.issue(ctx, operation, plan)
}

func (service *LinkIssuanceService) PasswordReset(
	ctx context.Context,
	actor model.LinkCreator,
	targetID string,
	version int64,
	key string,
) (model.AccountLink, bool, error) {
	operation, err := newAccountOperation(
		"postAdminUserPasswordResetLink",
		actor.UserID,
		key,
		map[string]any{
			"expectedVersion": version,
			"targetUserId":    targetID,
		},
		service.now().UnixMilli(),
	)
	if err != nil {
		return model.AccountLink{}, false, err
	}
	link, err := newAccountLink(actor, "PASSWORD_RESET", operation.Now)
	if err != nil {
		return model.AccountLink{}, false, err
	}
	link.TargetUserID = &targetID
	link.TargetVersion = version + 1
	plan := model.LinkIssuePlan{
		Link:           link,
		Target:         &model.LinkTarget{User: model.User{UserID: targetID}, Version: version},
		RevokePrevious: true,
	}
	return service.issue(ctx, operation, plan)
}

func newAccountLink(actor model.LinkCreator, kind string, now int64) (model.AccountLink, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.AccountLink{}, fmt.Errorf("create account link identity: %w", err)
	}
	return model.AccountLink{
		AccountLinkID: id.String(),
		Kind:          kind,
		CreatedBy:     &actor,
		State:         "ACTIVE",
		Version:       1,
		CreatedAtMS:   now,
		ExpiresAtMS:   now + int64(time.Hour/time.Millisecond),
	}, nil
}

func (service *LinkIssuanceService) issue(
	ctx context.Context,
	operation model.AccountOperation,
	plan model.LinkIssuePlan,
) (model.AccountLink, bool, error) {
	audit, err := auditLinkIssuance(plan)
	if err != nil {
		return model.AccountLink{}, false, err
	}
	body, err := json.Marshal(plan.Link)
	if err != nil {
		return model.AccountLink{}, false, fmt.Errorf("encode account link receipt: %w", err)
	}
	result, err := service.repository.CommitIssue(ctx, model.LinkIssueCommand{
		Operation: operation,
		Plan:      plan,
		Audit:     audit,
		Receipt:   accountReceipt(operation, 201, body),
	})
	if err != nil {
		return model.AccountLink{}, false,
			fmt.Errorf("commit account link issuance: %w", err)
	}
	id, err := uuid.Parse(result.Link.AccountLinkID)
	if err != nil {
		return model.AccountLink{}, false,
			fmt.Errorf("read issued link identity: %w", err)
	}
	result.Link.CapabilityToken = service.tokens.AccountLinkToken(result.Link.Kind, id)
	return result.Link, result.Replayed, nil
}

func auditLinkIssuance(plan model.LinkIssuePlan) (model.AccountAudit, error) {
	link := plan.Link
	action := "INVITATION_CREATED"
	after := map[string]any{
		"kind": "INVITATION", "role": link.Role,
		"expiresAtMs": link.ExpiresAtMS,
	}
	if link.Kind == "PASSWORD_RESET" {
		action = "PASSWORD_RESET_CREATED"
		after = map[string]any{
			"targetUserId": link.TargetUserID,
			"expiresAtMs":  link.ExpiresAtMS,
		}
	}
	return newAccountAudit(
		link.CreatedBy.UserID,
		action,
		"ACCOUNT_LINK",
		link.AccountLinkID,
		nil,
		after,
		link.CreatedAtMS,
	)
}
