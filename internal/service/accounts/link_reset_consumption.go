package accounts

import (
	"context"
	"fmt"

	model "retrom/internal/model/accounts"
)

type preparedReset struct {
	state        model.ResetState
	passwordHash string
	session      model.SessionMaterial
}

func (service *LinkConsumptionService) CompleteReset(
	ctx context.Context,
	request model.CompletePasswordResetRequest,
) (model.PasswordResetResult, error) {
	prepared, err := service.prepareReset(ctx, request)
	if err != nil {
		return model.PasswordResetResult{}, err
	}
	var result model.PasswordResetResult
	err = service.repository.WithConsumptionWrite(ctx, func(scope model.LinkConsumptionScope) error {
		now := service.options.Now().UnixMilli()
		current, err := recheckReset(ctx, scope.Read, prepared.state, now)
		if err != nil {
			return err
		}
		plan := model.ResetConsumption{
			LinkID:           current.Link.Link.AccountLinkID,
			LinkVersion:      current.Link.Link.Version,
			Target:           current.Target,
			PasswordHash:     prepared.passwordHash,
			ClearTestDefault: current.Target.User.Username == "test",
			Now:              now,
		}
		result = model.PasswordResetResult{Status: "PASSWORD_CHANGED_ACCOUNT_DISABLED"}
		if current.Target.Status == "ENABLED" {
			record := prepared.session.Record(current.Target.User.UserID, current.Target.SessionVersion+1, now)
			plan.Session = &record
			session := prepared.session.View(current.Target.User, current.Target.ProfileID, current.Target.SessionVersion+1, now)
			result = model.PasswordResetResult{Session: &session, Status: "AUTHENTICATED"}
		}
		if err := scope.Write.Reset(ctx, plan); err != nil {
			return fmt.Errorf("consume password reset: %w", err)
		}
		audit, err := newAccountAudit(
			current.Target.User.UserID,
			"PASSWORD_RESET_COMPLETED",
			"USER",
			current.Target.User.UserID,
			nil,
			map[string]any{
				"status": current.Target.Status,
			},
			now,
		)
		if err != nil {
			return err
		}
		if err := scope.Write.Audit(ctx, audit); err != nil {
			return fmt.Errorf("audit password reset: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.PasswordResetResult{}, fmt.Errorf("commit password reset consumption: %w", err)
	}
	return result, nil
}

func (service *LinkConsumptionService) prepareReset(
	ctx context.Context,
	request model.CompletePasswordResetRequest,
) (preparedReset, error) {
	id, valid := service.options.Tokens.ParseAccountLinkToken("PASSWORD_RESET", request.Token)
	if !valid {
		return preparedReset{}, model.ErrAccountLinkUnavailable
	}
	state, found, err := service.repository.ResetState(ctx, id.String())
	if err != nil {
		return preparedReset{}, fmt.Errorf("read password reset capability: %w", err)
	}
	if !activeLink(
		state.Link,
		found,
		"PASSWORD_RESET",
		service.options.Now().UnixMilli(),
	) || state.Target.Status == "DELETED" {
		return preparedReset{}, model.ErrAccountLinkUnavailable
	}
	hash, err := service.hashPassword(
		ctx,
		request.Password,
		request.PasswordConfirmation,
		state.Target.User.Username,
		state.Target.User.DisplayName,
	)
	if err != nil {
		return preparedReset{}, err
	}
	prepared := preparedReset{state: state, passwordHash: hash}
	if state.Target.Status == "ENABLED" {
		session, err := service.options.Mint()
		if err != nil {
			return preparedReset{}, fmt.Errorf("prepare reset session: %w", err)
		}
		prepared.session = session
	}
	return prepared, nil
}

func recheckReset(ctx context.Context, reader model.LinkConsumptionReader, before model.ResetState, now int64) (model.ResetState, error) {
	link, found, err := reader.Current(ctx, before.Link.Link.AccountLinkID)
	if err != nil {
		return model.ResetState{}, fmt.Errorf("recheck password reset link: %w", err)
	}
	if !activeLink(link, found, "PASSWORD_RESET", now) || link.Link.Version != before.Link.Link.Version {
		return model.ResetState{}, model.ErrAccountLinkUnavailable
	}
	if link.Link.TargetUserID == nil || *link.Link.TargetUserID != before.Target.User.UserID {
		return model.ResetState{}, model.ErrAccountLinkUnavailable
	}
	target, found, err := reader.Target(ctx, before.Target.User.UserID)
	if err != nil {
		return model.ResetState{}, fmt.Errorf("recheck password reset target: %w", err)
	}
	if !found || target.Status == "DELETED" || target != before.Target {
		return model.ResetState{}, model.ErrAccountLinkUnavailable
	}
	return model.ResetState{Link: link, Target: target}, nil
}
