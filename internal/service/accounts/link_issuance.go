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
	return service.issue(ctx, operation, func(_ model.LinkIssueScope) (model.LinkIssuePlan, error) {
		link, err := newAccountLink(actor, "INVITATION", operation.Now)
		if err != nil {
			return model.LinkIssuePlan{}, err
		}
		link.Role = &role
		return model.LinkIssuePlan{Link: link}, nil
	})
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
	return service.issue(ctx, operation, func(scope model.LinkIssueScope) (model.LinkIssuePlan, error) {
		target, found, err := scope.Read.Target(ctx, targetID)
		if err != nil {
			return model.LinkIssuePlan{}, fmt.Errorf("read password reset target: %w", err)
		}
		if err := validateLinkTarget(target, found, version); err != nil {
			return model.LinkIssuePlan{}, err
		}
		link, err := newAccountLink(actor, "PASSWORD_RESET", operation.Now)
		if err != nil {
			return model.LinkIssuePlan{}, err
		}
		link.TargetUserID = &targetID
		link.TargetVersion = target.Version + 1
		return model.LinkIssuePlan{Link: link, Target: &target, RevokePrevious: true}, nil
	})
}

func validateLinkTarget(target model.LinkTarget, found bool, version int64) error {
	if !found {
		return model.ErrUserNotFound
	}
	if target.Status == "DELETED" {
		return model.ErrUserDeleted
	}
	if target.Version != version {
		return model.ErrUserVersion
	}
	return nil
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
		ExpiresAtMS: now + int64(
			time.Hour/time.Millisecond,
		),
	}, nil
}

func (service *LinkIssuanceService) issue(
	ctx context.Context,
	operation model.AccountOperation,
	prepare func(model.LinkIssueScope) (model.LinkIssuePlan, error),
) (model.AccountLink, bool, error) {
	var result model.AccountLink
	var replayed bool
	err := service.repository.WithIssueWrite(ctx, func(scope model.LinkIssueScope) error {
		replay, err := scope.Read.Replay(ctx, operation)
		if err != nil {
			return fmt.Errorf("read account link replay: %w", err)
		}
		if err := checkAccountReplay(replay, operation); err != nil {
			return err
		}
		if replay.Found {
			replayed = true
			if err := json.Unmarshal(replay.Body, &result); err != nil {
				return fmt.Errorf("decode account link replay: %w", err)
			}
			return nil
		}
		plan, err := prepare(scope)
		if err != nil {
			return err
		}
		if err := scope.Write.Issue(ctx, plan); err != nil {
			return fmt.Errorf("issue account link: %w", err)
		}
		if err := auditLinkIssuance(ctx, scope.Write, plan); err != nil {
			return err
		}
		body, err := json.Marshal(plan.Link)
		if err != nil {
			return fmt.Errorf("encode account link receipt: %w", err)
		}
		if err := scope.Write.Remember(ctx, accountReceipt(operation, 201, body)); err != nil {
			return fmt.Errorf("remember account link: %w", err)
		}
		result = plan.Link
		return nil
	})
	if err != nil {
		return model.AccountLink{}, false, fmt.Errorf("commit account link issuance: %w", err)
	}
	id, err := uuid.Parse(result.AccountLinkID)
	if err != nil {
		return model.AccountLink{}, false, fmt.Errorf("read issued link identity: %w", err)
	}
	result.CapabilityToken = service.tokens.AccountLinkToken(result.Kind, id)
	return result, replayed, nil
}

func auditLinkIssuance(ctx context.Context, writer model.LinkIssueWriter, plan model.LinkIssuePlan) error {
	link := plan.Link
	action := "INVITATION_CREATED"
	after := map[string]any{"kind": "INVITATION", "role": link.Role, "expiresAtMs": link.ExpiresAtMS}
	if link.Kind == "PASSWORD_RESET" {
		action = "PASSWORD_RESET_CREATED"
		after = map[string]any{"targetUserId": link.TargetUserID, "expiresAtMs": link.ExpiresAtMS}
	}
	audit, err := newAccountAudit(
		link.CreatedBy.UserID,
		action,
		"ACCOUNT_LINK",
		link.AccountLinkID,
		nil,
		after,
		link.CreatedAtMS,
	)
	if err != nil {
		return err
	}
	if err := writer.Audit(ctx, audit); err != nil {
		return fmt.Errorf("audit account link issuance: %w", err)
	}
	return nil
}
