package payloadrelease

import (
	"context"
	"fmt"
)

type SourceBatch struct {
	Type     ScopeType
	ImportID string
}

type SourceReleaseReader interface {
	RetainedSources(context.Context, SourceBatch, string, int) ([]string, error)
	BoundSources(context.Context, string, Scope, int) ([]Scope, error)
}

type ReleaseScope struct {
	Scheduling SchedulingScope
	Links      SourceReleaseReader
}

type ReviewRelease struct {
	ItemID, ImportID string
	Reason           Reason
	NowMS            int64
}

func (service *Scheduler) Review(ctx context.Context, scope ReleaseScope, request ReviewRelease) error {
	if request.ItemID == "" || request.ImportID == "" || !validReason(request.Reason) || request.NowMS < 0 {
		return ErrScopeInvalid
	}
	if _, err := service.TerminalItem(ctx, scope.Scheduling, request.ItemID, request.Reason, request.NowMS); err != nil {
		return err
	}
	if err := service.boundSources(ctx, scope, request.ItemID, request.NowMS); err != nil {
		return err
	}
	if _, err := service.TerminalImport(ctx, scope.Scheduling, request.ImportID, request.NowMS); err != nil {
		return err
	}
	return nil
}

func (service *Scheduler) boundSources(ctx context.Context, scope ReleaseScope, itemID string, now int64) error {
	var cursor Scope
	for {
		sources, err := scope.Links.BoundSources(ctx, itemID, cursor, 200)
		if err != nil {
			return fmt.Errorf("read bound source release owners: %w", err)
		}
		if len(sources) == 0 {
			return nil
		}
		for _, source := range sources {
			if source.Type < cursor.Type || source.Type == cursor.Type && source.ID <= cursor.ID {
				return ErrScopeInvalid
			}
			if _, err := service.TerminalSource(ctx, scope.Scheduling, source, now); err != nil {
				return err
			}
			cursor = source
		}
	}
}

func (service *Scheduler) TerminalSources(ctx context.Context, scope ReleaseScope, batch SourceBatch, now int64) error {
	if batch.ImportID == "" || !isSourceItemScope(batch.Type) || now < 0 {
		return ErrScopeInvalid
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
				return ErrScopeInvalid
			}
			if _, err := service.TerminalSource(ctx, scope.Scheduling, Scope{Type: batch.Type, ID: id}, now); err != nil {
				return err
			}
			cursor = id
		}
	}
}

func isSourceItemScope(kind ScopeType) bool {
	return kind == ScopePegasusImportItem || kind == ScopeEmulationStationImportItem
}
