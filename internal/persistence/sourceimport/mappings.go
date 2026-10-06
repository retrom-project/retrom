package sourceimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/recordstore"
	tagrepository "retrom/internal/persistence/tagging"
	application "retrom/internal/service/sourceimport"
)

type Mappings struct{ database dbapi.DB }

func NewMappings(database dbapi.DB) *Mappings { return &Mappings{database: database} }
func (repository *Mappings) WithMappings(ctx context.Context, work func(application.MappingScope) error) error {
	err := dbapi.RetryTransaction(ctx, repository.database, func(tx dbapi.Tx) error {
		// Mapping is scoped to one locked Source in the default RR transaction.
		// Targets are share-locked, tag changes touch their versioned rows, and
		// Advance retains the Source version CAS in this same transaction.
		records := mappingRecords{executor: tx}
		if err := work(application.MappingScope{Read: records, Write: records, Tags: tagrepository.Bind(tx)}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit Source mappings: %w", err)
	}
	return nil
}

type mappingRecords struct{ executor dbapi.Executor }

func (records mappingRecords) Import(ctx context.Context, id string) (application.Summary, error) {
	value, err := scanSummary(dbapi.QueryRowContext(ctx, records.executor,
		summaryQuery+` WHERE import.id=? FOR UPDATE OF import`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return application.Summary{}, application.ErrNotFound
	}
	return value, err
}

func (records mappingRecords) CollectionOwners(ctx context.Context, ids []string) (map[string]string, error) {
	rows, err := records.executor.QueryContext(ctx,
		`SELECT id,import_id FROM source_import_collections WHERE id=ANY(?::text[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("read Source collection owners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make(map[string]string, len(ids))
	for rows.Next() {
		var id, owner string
		if err := rows.Scan(&id, &owner); err != nil {
			return nil, fmt.Errorf("scan Source collection owner: %w", err)
		}
		result[id] = owner
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Source collection owners: %w", err)
	}
	return result, nil
}

func (records mappingRecords) EligibleTarget(ctx context.Context, id string) (application.MappingTarget, bool, error) {
	var result application.MappingTarget
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT instance.id,instance.version,instance.platform_id,instance.default_core_id,target.provider_id,target.target_id,
(SELECT id FROM dat_versions WHERE provider_id=target.provider_id AND target_id=target.target_id AND is_active=1
 FOR SHARE)
FROM platform_instances instance
JOIN platforms platform ON platform.id=instance.platform_id AND platform.enabled=1
JOIN cores core ON core.id=instance.default_core_id AND core.enabled=1
JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id AND binding.launch_policy!='DISABLED'
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE instance.id=? AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
FOR SHARE OF instance,platform,core,binding,binding_platform,target`, id).
		Scan(
			&result.InstanceID,
			&result.InstanceVersion,
			&result.PlatformID,
			&result.CoreID,
			&result.ProviderID,
			&result.TargetID,
			&result.DATVersionID,
		)
	if errors.Is(err, sql.ErrNoRows) {
		return application.MappingTarget{}, false, nil
	}
	if err != nil {
		return application.MappingTarget{}, false, fmt.Errorf("query Source eligible target: %w", err)
	}
	return result, true, nil
}

func (records mappingRecords) Put(ctx context.Context, change application.CollectionMapping) error {
	tags, err := json.Marshal(change.Tags)
	if err != nil {
		return fmt.Errorf("encode Source mapping tags: %w", err)
	}
	values := make([]any, 0, 10)
	values = append(values, change.Mapping.Action, string(tags))
	values = append(values, mappingTargetValues(change.Target)...)
	values = append(values, change.NowMS)
	result, err := recordstore.UpdateSourceImportCollections(ctx, records.executor, recordstore.Update{
		Set: `mapping_action=?,tag_snapshot_json=?,target_platform_instance_id=?,target_platform_instance_version=?,
target_platform_id=?,target_default_core_id=?,target_provider_id=?,target_id=?,target_dat_version_id=?,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND import_id=?`, Args: []any{change.Mapping.CollectionID, change.ImportID}},
		Values: values,
	})
	return requireWorkflowChange(result, err, application.ErrInvalid)
}

func mappingTargetValues(target *application.MappingTarget) []any {
	if target == nil {
		return []any{nil, nil, nil, nil, nil, nil, nil}
	}
	return []any{
		target.InstanceID,
		target.InstanceVersion,
		target.PlatformID,
		target.CoreID,
		target.ProviderID,
		target.TargetID,
		target.DATVersionID,
	}
}

func (records mappingRecords) Advance(ctx context.Context, change application.MappingAdvance) error {
	result, err := recordstore.UpdateSourceImports(ctx, records.executor, recordstore.Update{
		Set: `mapped_collection_count=(SELECT count(*) FROM source_import_collections
WHERE import_id=? AND mapping_action='IMPORT'),
skipped_collection_count=(SELECT count(*) FROM source_import_collections WHERE import_id=? AND mapping_action='SKIP'),
mapping_version=mapping_version+1,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=? AND mapping_version=? AND state='AWAITING_MAPPING'`, Args: []any{
			change.Before.ID, change.Before.Version, change.Before.MappingVersion,
		}},
		Values: []any{change.Before.ID, change.Before.ID, change.NowMS},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
