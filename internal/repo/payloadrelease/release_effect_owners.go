package payloadrelease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/payloadrelease"
)

func (records effectRecords) Owner(ctx context.Context, scope application.Scope) (application.EffectOwner, error) {
	if scope.Type == application.ScopeUploadConsumption {
		return records.consumptionOwner(ctx, scope)
	}
	owner, err := scheduling(records).Owner(ctx, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return application.EffectOwner{Owner: application.Owner{Scope: scope}}, nil
	}
	if err != nil {
		return application.EffectOwner{}, fmt.Errorf("read effect owner: %w", err)
	}
	facts := application.EffectOwner{Owner: owner, Found: true}
	if err := records.ownerRelations(ctx, &facts); err != nil {
		return application.EffectOwner{}, err
	}
	return facts, nil
}

func (records effectRecords) ownerRelations(ctx context.Context, facts *application.EffectOwner) error {
	var err error
	scope := facts.Owner.Scope
	switch scope.Type {
	case application.ScopeGame:
		err = records.executor.QueryRowContext(ctx, `SELECT
metadata_source_kind,COALESCE(metadata_source_ref_id,''),content_source_kind,COALESCE(content_source_ref_id,'')
FROM games WHERE id=?`, scope.ID).Scan(
			&facts.MetadataSource.Kind,
			&facts.MetadataSource.ID,
			&facts.ContentSource.Kind,
			&facts.ContentSource.ID,
		)
	case application.ScopeImportItem:
		err = records.executor.QueryRowContext(ctx, `SELECT import_job_id FROM import_items WHERE id=?`, scope.ID).Scan(
			&facts.ParentID,
		)
	case application.ScopePegasusImportItem, application.ScopeEmulationStationImportItem:
		spec, specErr := effectSourceSpec(scope.Type)
		if specErr != nil {
			return specErr
		}
		query := `SELECT import_id,COALESCE(existing_game_id,''),EXISTS(
SELECT 1 FROM import_items item JOIN import_item_duplicate_matches duplicate ON duplicate.import_item_id=item.id
WHERE item.id=source.library_import_item_id AND item.state='DISCARDED' AND
duplicate.existing_game_id=source.existing_game_id)
FROM ` + spec.itemsTable + ` source WHERE source.id=?`
		err = records.executor.QueryRowContext(ctx, query, scope.ID).Scan(
			&facts.ParentID,
			&facts.ExistingGameID,
			&facts.DuplicateMatch,
		)
	case application.ScopeImportJob:
		return nil
	case application.ScopeUploadConsumption, application.ScopeBlob:
		return application.ErrScopeInvalid
	default:
		return application.ErrScopeInvalid
	}
	if err != nil {
		return fmt.Errorf("read effect owner relations: %w", err)
	}
	return nil
}

func (records effectRecords) consumptionOwner(
	ctx context.Context,
	scope application.Scope,
) (application.EffectOwner, error) {
	var facts application.EffectOwner
	facts.Owner.Scope = scope
	facts.Consumption.ID = scope.ID
	var released sql.NullInt64
	err := records.executor.QueryRowContext(ctx, `SELECT version,released_at_ms,upload_session_id FROM upload_consumptions
WHERE id=?`, scope.ID).Scan(

		&facts.Consumption.Version,

		&released,

		&facts.Consumption.SessionID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return application.EffectOwner{}, fmt.Errorf("read effect consumption: %w", err)
	}
	if err := records.executor.QueryRowContext(ctx, `SELECT COALESCE(upload_file_id,''),consumer_type,consumer_id
FROM upload_consumptions
WHERE id=?`, scope.ID).Scan(

		&facts.Consumption.FileID,

		&facts.Consumption.ConsumerType,

		&facts.Consumption.ConsumerID,
	); err != nil {
		return application.EffectOwner{}, fmt.Errorf("read effect consumption owner: %w", err)
	}
	facts.Found = true
	facts.Consumption.Released = application.WorkTime{Set: released.Valid, Value: released.Int64}
	facts.Owner.Version = facts.Consumption.Version
	return facts, nil
}

func (records effectRecords) fenceOwner(ctx context.Context, before application.EffectOwner) error {
	current, err := records.Owner(ctx, before.Owner.Scope)
	if err != nil {
		return fmt.Errorf("reread effect owner: %w", err)
	}
	if !current.Found || current != before {
		return application.ErrEffectConflict
	}
	return nil
}

func (records effectRecords) Links(ctx context.Context, scope application.Scope) ([]application.Scope, error) {
	if scope.Type == application.ScopeImportJob {
		ids, err := collectIDs(
			ctx,
			records.executor,
			`SELECT id FROM import_items WHERE import_job_id=? ORDER BY id`,
			scope.ID,
		)
		if err != nil {
			return nil, err
		}
		links := make([]application.Scope, 0, len(ids))
		for _, id := range ids {
			links = append(links, application.Scope{Type: application.ScopeImportItem, ID: id})
		}
		return links, nil
	}
	if scope.Type == application.ScopeImportItem {
		return records.boundSources(ctx, scope.ID)
	}
	return nil, application.ErrScopeInvalid
}

func (records effectRecords) boundSources(ctx context.Context, id string) ([]application.Scope, error) {
	links := make([]application.Scope, 0)
	for _, kind := range []application.ScopeType{
		application.ScopePegasusImportItem,
		application.ScopeEmulationStationImportItem,
	} {
		spec, err := effectSourceSpec(kind)
		if err != nil {
			return nil, err
		}
		ids, err := collectIDs(
			ctx,
			records.executor,
			`SELECT id FROM `+spec.itemsTable+` WHERE library_import_item_id=? ORDER BY id`,
			id,
		)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			links = append(links, application.Scope{Type: kind, ID: id})
		}
	}
	return links, nil
}
