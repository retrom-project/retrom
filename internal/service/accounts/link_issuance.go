package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type LinkIssuanceService struct {
	repository LinkIssueRepository
	tokens     LinkIssuer
	now        func() time.Time
}

func NewLinkIssuance(repository LinkIssueRepository, tokens LinkIssuer, now func() time.Time) *LinkIssuanceService {
	return &LinkIssuanceService{repository, tokens, now}
}

func (service *LinkIssuanceService) Invitation(
	ctx context.Context,
	actor LinkCreator,
	role string,
	confirmed bool,
	key string,
) (AccountLink, bool, error) {
	if role != "USER" && role != "ADMIN" || (role == "ADMIN") != confirmed {
		return AccountLink{}, false, ErrRoleConfirmation
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
		return AccountLink{}, false, err
	}
	return service.issue(ctx, operation, func(_ LinkIssueScope) (LinkIssuePlan, error) {
		link, err := newAccountLink(actor, "INVITATION", operation.Now)
		if err != nil {
			return LinkIssuePlan{}, err
		}
		link.Role = &role
		return LinkIssuePlan{Link: link}, nil
	})
}

func (service *LinkIssuanceService) PasswordReset(
	ctx context.Context,
	actor LinkCreator,
	targetID string,
	version int64,
	key string,
) (AccountLink, bool, error) {
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
		return AccountLink{}, false, err
	}
	return service.issue(ctx, operation, func(scope LinkIssueScope) (LinkIssuePlan, error) {
		target, found, err := scope.Read.Target(ctx, targetID)
		if err != nil {
			return LinkIssuePlan{}, fmt.Errorf("read password reset target: %w", err)
		}
		if err := validateLinkTarget(target, found, version); err != nil {
			return LinkIssuePlan{}, err
		}
		link, err := newAccountLink(actor, "PASSWORD_RESET", operation.Now)
		if err != nil {
			return LinkIssuePlan{}, err
		}
		link.TargetUserID = &targetID
		link.TargetVersion = target.Version + 1
		return LinkIssuePlan{Link: link, Target: &target, RevokePrevious: true}, nil
	})
}

func validateLinkTarget(target LinkTarget, found bool, version int64) error {
	if !found {
		return ErrUserNotFound
	}
	if target.Status == "DELETED" {
		return ErrUserDeleted
	}
	if target.Version != version {
		return ErrUserVersion
	}
	return nil
}

func newAccountLink(actor LinkCreator, kind string, now int64) (AccountLink, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return AccountLink{}, fmt.Errorf("create account link identity: %w", err)
	}
	return AccountLink{
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
	operation AccountOperation,
	prepare func(LinkIssueScope) (LinkIssuePlan, error),
) (AccountLink, bool, error) {
	var result AccountLink
	var replayed bool
	err := service.repository.WithIssueWrite(ctx, func(scope LinkIssueScope) error {
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
		return AccountLink{}, false, fmt.Errorf("commit account link issuance: %w", err)
	}
	id, err := uuid.Parse(result.AccountLinkID)
	if err != nil {
		return AccountLink{}, false, fmt.Errorf("read issued link identity: %w", err)
	}
	result.CapabilityToken = service.tokens.AccountLinkToken(result.Kind, id)
	return result, replayed, nil
}

func auditLinkIssuance(ctx context.Context, writer LinkIssueWriter, plan LinkIssuePlan) error {
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
