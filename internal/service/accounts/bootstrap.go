package accounts

import (
	"context"
	"fmt"

	model "retrom/internal/model/accounts"

	"github.com/google/uuid"
)

func (service *InitializationService) bootstrap(
	ctx context.Context,
	username, display, password, kind string,
) (model.Session, error) {
	hash, err := service.options.Hasher.Hash(ctx, password)
	if err != nil {
		return model.Session{}, fmt.Errorf("hash initial password: %w", err)
	}
	material, err := service.options.Mint()
	if err != nil {
		return model.Session{}, fmt.Errorf("prepare initial session: %w", err)
	}
	plan, err := bootstrapPlan(username, display, hash, kind)
	if err != nil {
		return model.Session{}, err
	}
	err = service.repository.CommitWrite(ctx, func(scope model.InitializationScope) error {
		state, err := scope.Read.State(ctx)
		if err != nil {
			return fmt.Errorf("recheck initialization state: %w", err)
		}
		if state.State != "PENDING" {
			return model.ErrInitializationDone
		}
		if state.Users != 0 || state.Profiles != 0 {
			return model.ErrInitializationState
		}
		plan.Now = service.options.Now().UnixMilli()
		plan.Session = material.Record(plan.UserID, 1, plan.Now)
		if err := scope.Write.Bootstrap(ctx, plan); err != nil {
			return fmt.Errorf("initialize account store: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Session{}, fmt.Errorf("commit account initialization: %w", err)
	}
	return material.View(
		model.User{
			UserID:      plan.UserID,
			Username:    username,
			DisplayName: display,
			Role:        "ADMIN",
		},
		plan.ProfileID,
		1,
		plan.Now,
	), nil
}

func bootstrapPlan(username, display, hash, kind string) (model.BootstrapPlan, error) {
	ids := make([]string, 3)
	for i := range ids {
		value, err := uuid.NewV7()
		if err != nil {
			return model.BootstrapPlan{}, fmt.Errorf("create initialization identity: %w", err)
		}
		ids[i] = value.String()
	}
	plan := model.BootstrapPlan{
		UserID:       ids[0],
		ProfileID:    ids[1],
		AuditID:      ids[2],
		Username:     username,
		DisplayName:  display,
		PasswordHash: hash,
		Kind:         kind,
		ActorLabel:   "release-setup",
	}
	if kind == "TEST_DEFAULT" {
		plan.TestDefault = true
		plan.ActorLabel = "startup-test-bootstrap"
	}
	return plan, nil
}
