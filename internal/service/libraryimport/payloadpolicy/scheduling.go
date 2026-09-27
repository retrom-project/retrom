package payloadpolicy

import (
	"context"
	"fmt"

	jobs "retrom/internal/service/cleanupjobs"
)

type ReviewRelease struct {
	ItemID, ImportID string
	Reason           jobs.Reason
	NowMS            int64
}

func TerminalItem(ctx context.Context, service *jobs.Scheduler,
	scope jobs.OwnerSchedulingScope, id string, reason jobs.Reason, now int64,
) (string, error) {
	owner, err := jobs.ReadSchedulingOwner(ctx, scope, jobs.Scope{Type: jobs.ScopeImportItem, ID: id})
	if err != nil {
		return "", fmt.Errorf("schedule libraryimport cleanup: %w", err)
	}
	if !ItemTerminal(owner.State) {
		return "", jobs.ErrScopeInvalid
	}
	value, err := service.ScheduleOwner(ctx, scope, owner, reason, now)
	if err != nil {
		return "", fmt.Errorf("complete libraryimport cleanup: %w", err)
	}
	return value, nil
}

func TerminalImport(ctx context.Context, service *jobs.Scheduler, scope jobs.ItemSchedulingScope, id string, now int64,
) (string, error) {
	pending, err := scope.PendingChildren(ctx, id)
	if err != nil {
		return "", fmt.Errorf("read pending release children: %w", err)
	}
	if pending > 0 {
		return "", nil
	}
	owner, err := jobs.ReadSchedulingOwner(ctx, scope, jobs.Scope{Type: jobs.ScopeImportJob, ID: id})
	if err != nil {
		return "", fmt.Errorf("schedule libraryimport cleanup: %w", err)
	}
	if !JobTerminal(owner.State) {
		return "", nil
	}
	value, err := service.ScheduleOwner(ctx, scope, owner, jobs.ReasonImportTerminal, now)
	if err != nil {
		return "", fmt.Errorf("complete libraryimport cleanup: %w", err)
	}
	return value, nil
}

func Review(ctx context.Context, service *jobs.Scheduler, scope jobs.ItemSchedulingScope, request ReviewRelease) error {
	if request.ItemID == "" || request.ImportID == "" || !jobs.ValidReason(request.Reason) || request.NowMS < 0 {
		return jobs.ErrScopeInvalid
	}
	if _, err := TerminalItem(ctx, service, scope, request.ItemID, request.Reason, request.NowMS); err != nil {
		return err
	}
	if _, err := TerminalImport(ctx, service, scope, request.ImportID, request.NowMS); err != nil {
		return err
	}
	return nil
}
