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
	now := service.options.Now().UnixMilli()
	state := prepared.state
	plan := model.ResetConsumption{
		LinkID:           state.Link.Link.AccountLinkID,
		LinkVersion:      state.Link.Link.Version,
		Target:           state.Target,
		PasswordHash:     prepared.passwordHash,
		ClearTestDefault: state.Target.User.Username == "test",
		Now:              now,
	}
	result := model.PasswordResetResult{Status: "PASSWORD_CHANGED_ACCOUNT_DISABLED"}
	if state.Target.Status == "ENABLED" {
		record := prepared.session.Record(
			state.Target.User.UserID,
			state.Target.SessionVersion+1,
			now,
		)
		plan.Session = &record
		session := prepared.session.View(
			state.Target.User,
			state.Target.ProfileID,
			state.Target.SessionVersion+1,
			now,
		)
		result = model.PasswordResetResult{Session: &session, Status: "AUTHENTICATED"}
	}
	audit, err := newAccountAudit(
		state.Target.User.UserID,
		"PASSWORD_RESET_COMPLETED",
		"USER",
		state.Target.User.UserID,
		nil,
		map[string]any{
			"status": state.Target.Status,
		},
		now,
	)
	if err != nil {
		return model.PasswordResetResult{}, err
	}
	if err := service.repository.CommitPasswordReset(
		ctx, model.PasswordResetCommand{Plan: plan, Audit: audit},
	); err != nil {
		return model.PasswordResetResult{},
			fmt.Errorf("commit password reset consumption: %w", err)
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
	if !model.ActiveLink(
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
