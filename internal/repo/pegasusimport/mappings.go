package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
	tagrepository "retrom/internal/repo/tagging"
	application "retrom/internal/service/pegasusimport"
)

type Mappings struct{ database *sql.DB }

func NewMappings(database *sql.DB) *Mappings { return &Mappings{database: database} }
func (repository *Mappings) WithMappings(ctx context.Context, work func(application.MappingScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Pegasus mappings: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := mappingRecords{executor: tx}
	if err := work(application.MappingScope{Read: records, Write: records, Tags: tagrepository.Bind(tx)}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Pegasus mappings: %w", err)
	}
	return nil
}

type mappingRecords struct{ executor dbexec.Executor }

func (records mappingRecords) Import(ctx context.Context, id string) (application.Summary, error) {
	return (&Queries{database: records.executor}).Get(ctx, id)
}

func (records mappingRecords) CollectionOwner(ctx context.Context, id string) (string, error) {
	var result string
	err := records.executor.QueryRowContext(ctx, `SELECT import_id FROM pegasus_import_collections WHERE id=?`, id).Scan(
		&result,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return "", application.ErrInvalid
	}
	if err != nil {
		return "", fmt.Errorf("read Pegasus collection owner: %w", err)
	}
	return result, nil
}

func (records mappingRecords) EligibleTarget(ctx context.Context, id string) (application.MappingTarget, bool, error) {
	var result application.MappingTarget
	err := records.executor.QueryRowContext(ctx, `
SELECT instance.id,instance.version,instance.platform_id,instance.default_core_id,target.provider_id,target.target_id,
(SELECT id FROM dat_versions WHERE provider_id=target.provider_id AND target_id=target.target_id AND is_active=1)
FROM platform_instances instance
JOIN platforms platform ON platform.id=instance.platform_id AND platform.enabled=1
JOIN cores core ON core.id=instance.default_core_id AND core.enabled=1
JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id AND binding.launch_policy!='DISABLED'
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE instance.id=? AND instance.enabled=1 AND instance.deleted_at_ms IS NULL`, id).
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
		return application.MappingTarget{}, false, fmt.Errorf("query Pegasus eligible target: %w", err)
	}
	return result, true, nil
}

func (records mappingRecords) Put(ctx context.Context, change application.CollectionMapping) error {
	tags, err := json.Marshal(change.Tags)
	if err != nil {
		return fmt.Errorf("encode Pegasus mapping tags: %w", err)
	}
	values := make([]any, 0, 10)
	values = append(values, change.Mapping.Action, string(tags))
	values = append(values, mappingTargetValues(change.Target)...)
	values = append(values, change.NowMS)
	result, err := recordstore.UpdatePegasusImportCollections(ctx, records.executor, recordstore.Update{
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
	result, err := recordstore.UpdatePegasusImports(ctx, records.executor, recordstore.Update{
		Set: `mapped_collection_count=(SELECT count(*) FROM pegasus_import_collections
WHERE import_id=? AND mapping_action='IMPORT'),
skipped_collection_count=(SELECT count(*) FROM pegasus_import_collections WHERE import_id=? AND mapping_action='SKIP'),
mapping_version=mapping_version+1,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND version=? AND mapping_version=? AND state='AWAITING_MAPPING'`, Args: []any{
			change.Before.ID, change.Before.Version, change.Before.MappingVersion,
		}},
		Values: []any{change.Before.ID, change.Before.ID, change.NowMS},
	})
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
