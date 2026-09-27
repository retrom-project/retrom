package payloadpolicy

import (
	"context"
	"fmt"

	jobs "retrom/internal/service/cleanupjobs"
)

type SourceBatch struct {
	Type     jobs.ScopeType
	ImportID string
}

type SourceReleaseReader interface {
	RetainedSources(context.Context, SourceBatch, string, int) ([]string, error)
}

type ReleaseScope struct {
	Scheduling jobs.OwnerSchedulingScope
	Links      SourceReleaseReader
}

func TerminalSource(ctx context.Context, service *jobs.Scheduler,
	scope jobs.OwnerSchedulingScope, ref jobs.Scope, now int64,
) (string, error) {
	if ref.Type != jobs.ScopeSourceImportItem {
		return "", jobs.ErrScopeInvalid
	}
	owner, err := jobs.ReadSchedulingOwner(ctx, scope, ref)
	if err != nil {
		return "", fmt.Errorf("schedule sourceimport cleanup: %w", err)
	}
	if !ReleaseReady(owner.State, owner.Retryable, owner.PublicID) {
		return "", nil
	}
	if owner.PayloadState != "RETAINED" {
		value, err := jobs.ExistingOwnerRelease(owner)
		if err != nil {
			return "", fmt.Errorf("complete sourceimport cleanup: %w", err)
		}
		return value, nil
	}
	reason := jobs.ReasonSourceTerminal
	value, err := service.ScheduleOwner(ctx, scope, owner, reason, now)
	if err != nil {
		return "", fmt.Errorf("complete sourceimport cleanup: %w", err)
	}
	return value, nil
}

func TerminalSources(ctx context.Context, service *jobs.Scheduler, scope ReleaseScope,
	batch SourceBatch, now int64,
) error {
	if batch.ImportID == "" || !isSourceItemScope(batch.Type) || now < 0 {
		return jobs.ErrScopeInvalid
	}
	cursor := ""
	for {
		ids, err := scope.Links.RetainedSources(ctx, batch, cursor, 200)
		if err != nil {
			return fmt.Errorf("read source payload owners: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			if id <= cursor {
				return jobs.ErrScopeInvalid
			}
			if _, err := TerminalSource(ctx, service, scope.Scheduling, jobs.Scope{Type: batch.Type, ID: id}, now); err != nil {
				return err
			}
			cursor = id
		}
	}
}

func isSourceItemScope(kind jobs.ScopeType) bool { return kind == jobs.ScopeSourceImportItem }
