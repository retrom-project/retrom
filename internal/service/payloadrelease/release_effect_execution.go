package payloadrelease

import (
	"context"
	"fmt"
	"time"
)

func (service *ReleaseEffects) Execute(ctx context.Context, unit Execution) error {
	frozen, err := DecodeWork(unit.Work)
	if err != nil {
		return err
	}
	if frozen != unit.Input || unit.Work.Kind != "PAYLOAD_RELEASE" {
		return ErrInputInvalid
	}
	if unit.Work.Scope.Type == ScopeGame {
		if err := service.waitForMutations(ctx, unit.Work.Scope); err != nil {
			return err
		}
	}
	err = service.repository.WithEffects(ctx, func(scope EffectScope) error {
		if err := service.authority.CheckInScope(ctx, scope.Worker, unit.Work); err != nil {
			return fmt.Errorf("check reference release authority: %w", err)
		}
		before, err := scope.Read.Owner(ctx, unit.Work.Scope)
		if err != nil {
			return fmt.Errorf("read release owner: %w", err)
		}
		if err := validateEffectRoot(unit, before); err != nil {
			return err
		}
		if err := recheckEffectMutations(ctx, scope.Read, unit.Work.Scope); err != nil {
			return err
		}
		run := effectRun{scope: scope, nowMS: service.now().UnixMilli(), visited: make(map[Scope]bool)}
		if err := run.execute(ctx, before, unit.Input.Inputs.Reason); err != nil {
			return err
		}
		if err := service.gc.StageInScope(ctx, scope.GC, run.blobs); err != nil {
			return fmt.Errorf("stage released payload: %w", err)
		}
		if err := run.confirm(ctx); err != nil {
			return err
		}
		if err := service.authority.CheckInScope(ctx, scope.Worker, unit.Work); err != nil {
			return fmt.Errorf("confirm reference release authority: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit payload references: %w", err)
	}
	return nil
}

func (service *ReleaseEffects) waitForMutations(ctx context.Context, scope Scope) error {
	for {
		active, err := service.repository.ActiveMutations(ctx, scope)
		if err != nil {
			return fmt.Errorf("read game mutations: %w", err)
		}
		if active == 0 {
			return nil
		}
		if err := service.waiter.Wait(ctx, 100*time.Millisecond); err != nil {
			return fmt.Errorf("wait for game mutations: %w", err)
		}
	}
}

type effectRun struct {
	scope     EffectScope
	nowMS     int64
	blobs     []string
	visited   map[Scope]bool
	completed []EffectOwner
}

func (run *effectRun) release(ctx context.Context, before EffectOwner) error {
	owner := before.Owner
	if owner.PayloadState == "RELEASED" || run.visited[owner.Scope] {
		return nil
	}
	run.visited[owner.Scope] = true
	var err error
	if before, err = run.retry(ctx, before); err != nil {
		return err
	}
	switch owner.Scope.Type {
	case ScopeGame:
		return run.game(ctx, before)
	case ScopeImportItem:
		return run.item(ctx, before)
	case ScopeImportJob:
		return run.aggregate(ctx, before)
	case ScopeSourceImportItem:
		return run.source(ctx, before)
	case ScopeUploadConsumption, ScopeBlob:
		return ErrScopeInvalid
	default:
		return ErrScopeInvalid
	}
}
