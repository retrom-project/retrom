package payloadpolicy

import (
	"context"
	"fmt"

	jobs "retrom/internal/service/cleanupjobs"
	uploadcleanup "retrom/internal/service/uploads/payloadpolicy"
)

func Release(ctx context.Context, scope jobs.EffectScope, unit jobs.Execution, now int64) (bool, error) {
	before, err := scope.Read.Owner(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read import cleanup owner: %w", err)
	}
	if err := ValidateRelease(unit, before); err != nil {
		return false, fmt.Errorf("release libraryimport payload: %w", err)
	}
	if before.Owner.PayloadState == "RELEASED" {
		return false, nil
	}
	if unit.Work.Scope.Type == jobs.ScopeImportJob {
		if err := RequireReleasedItems(ctx, scope, before); err != nil {
			return false, fmt.Errorf("release libraryimport payload: %w", err)
		}
	}
	initial, err := scope.Read.Remaining(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read import payload: %w", err)
	}
	payload, err := scope.Read.Payload(ctx, unit.Work.Scope)
	if err != nil {
		return false, fmt.Errorf("read import consumptions: %w", err)
	}
	if err := uploadcleanup.ConsumePayload(ctx, scope, payload, ReleaseReason(before.Owner), now); err != nil {
		return false, fmt.Errorf("release libraryimport payload: %w", err)
	}
	if err := scope.Write.Clear(ctx, before, now); err != nil {
		return false, fmt.Errorf("clear import payload: %w", err)
	}
	more, err := uploadcleanup.PurgePayload(ctx, scope, payload, now)
	if err != nil {
		return false, fmt.Errorf("release libraryimport payload: %w", err)
	}
	value, err := jobs.FinishRelease(ctx, scope, before, initial, more, false, now)
	if err != nil {
		return false, fmt.Errorf("complete libraryimport cleanup: %w", err)
	}
	return value, nil
}

func ValidateRelease(unit jobs.Execution, before jobs.EffectOwner) error {
	terminal := ItemTerminal(before.Owner.State)
	if unit.Work.Scope.Type == jobs.ScopeImportJob {
		terminal = JobTerminal(before.Owner.State)
	}
	if !terminal {
		return jobs.ErrOwnerNotTerminal
	}
	if err := jobs.CheckReleaseOwner(unit, before, false); err != nil {
		return fmt.Errorf("validate import cleanup: %w", err)
	}
	return nil
}

func ReleaseReason(owner jobs.Owner) jobs.Reason {
	if owner.Scope.Type == jobs.ScopeImportJob {
		return jobs.ReasonImportTerminal
	}
	switch owner.State {
	case "PUBLISHED":
		return jobs.ReasonImportPublished
	case "DISCARDED":
		return jobs.ReasonImportDiscarded
	case "CANCELLED":
		return jobs.ReasonImportCancelled
	default:
		return jobs.ReasonImportFailed
	}
}

func RequireReleasedItems(ctx context.Context, scope jobs.EffectScope, before jobs.EffectOwner) error {
	links, err := scope.Read.Links(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("read import items: %w", err)
	}
	for _, link := range links {
		child, err := scope.Read.Owner(ctx, link)
		if err != nil {
			return fmt.Errorf("read import item: %w", err)
		}
		if !child.Found || child.ParentID != before.Owner.Scope.ID || !ItemTerminal(child.Owner.State) ||
			child.Owner.PayloadState != "RELEASED" || child.Owner.ReleaseJobID == "" {
			return jobs.ErrDependencyPending
		}
	}
	return nil
}
