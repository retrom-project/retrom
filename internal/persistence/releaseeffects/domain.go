package releaseeffects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/releaseops"
	application "retrom/internal/service/cleanupjobs"
)

type Domain struct {
	Owner     func(context.Context, application.Scope) (application.EffectOwner, error)
	Payload   func(context.Context, application.Scope) (application.EffectPayload, error)
	Links     func(context.Context, application.Scope) ([]application.Scope, error)
	Remaining func(context.Context, application.Scope) (int64, error)
	Change    func(context.Context, recordstore.Update, application.EffectOwnerChange) (sql.Result, error)
	Remove    func(context.Context, application.EffectRemoval) error
}
type Uploads struct {
	Owner   func(context.Context, application.Scope) (application.EffectOwner, error)
	Files   application.EffectUploads
	Consume func(context.Context, application.EffectConsumptionChange) error
}
type records struct {
	executor dbapi.Executor
	domain   Domain
	uploads  Uploads
}

func ReadOwner(ctx context.Context, executor dbapi.Executor, scope application.Scope,
	read func(context.Context, dbapi.Executor, application.Scope) (application.Owner, error),
	relations func(context.Context, dbapi.Executor, *application.EffectOwner) error,
) (application.EffectOwner, error) {
	owner, err := read(ctx, executor, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return application.EffectOwner{Owner: application.Owner{Scope: scope}}, nil
	}
	if err != nil {
		return application.EffectOwner{}, fmt.Errorf("read domain owner: %w", err)
	}
	facts := application.EffectOwner{Owner: owner, Found: true}
	if relations != nil {
		if err := relations(ctx, executor, &facts); err != nil {
			return application.EffectOwner{}, fmt.Errorf("read domain relations: %w", err)
		}
	}
	return facts, nil
}

func (records records) Owner(ctx context.Context, scope application.Scope) (application.EffectOwner, error) {
	read := records.domain.Owner
	if scope.Type == application.ScopeUploadConsumption {
		read = records.uploads.Owner
	}
	result, err := read(ctx, scope)
	if err != nil {
		return result, fmt.Errorf("read effect owner: %w", err)
	}
	return result, nil
}

func (records records) fence(ctx context.Context, before application.EffectOwner) error {
	current, err := records.Owner(ctx, before.Owner.Scope)
	if err != nil {
		return err
	}
	if !current.Found || current != before {
		return application.ErrEffectConflict
	}
	return nil
}

func (records records) ChangeOwner(ctx context.Context, change application.EffectOwnerChange) error {
	if err := records.fence(ctx, change.Before); err != nil {
		return err
	}
	if records.domain.Change == nil {
		return application.ErrScopeInvalid
	}
	before, after := change.Before.Owner, change.After
	var released any
	if change.Released {
		released = change.NowMS
	}
	update := recordstore.Update{
		Set:    `payload_state=?,payload_release_job_id=?,version=?,payload_released_at_ms=?`,
		Values: []any{after.PayloadState, after.ReleaseJobID, after.Version, released}, Scope: recordstore.Scope{
			Where: `id=? AND version=? AND payload_state=? AND COALESCE(payload_release_job_id,'')=?`,
			Args:  []any{before.Scope.ID, before.Version, before.PayloadState, before.ReleaseJobID},
		},
	}
	result, err := records.domain.Change(ctx, update, change)
	if err := releaseops.Count(result, err, 1); err != nil {
		return fmt.Errorf("change domain payload owner: %w", err)
	}
	return nil
}

func (records records) Remove(ctx context.Context, change application.EffectRemoval) error {
	if err := records.fence(ctx, change.Before); err != nil {
		return err
	}
	if records.domain.Remove == nil {
		return application.ErrScopeInvalid
	}
	if err := records.domain.Remove(ctx, change); err != nil {
		return fmt.Errorf("remove domain payload: %w", err)
	}
	return nil
}

func (records records) Consume(ctx context.Context, change application.EffectConsumptionChange) error {
	if err := records.uploads.Consume(ctx, change); err != nil {
		return fmt.Errorf("release upload consumption: %w", err)
	}
	return nil
}
