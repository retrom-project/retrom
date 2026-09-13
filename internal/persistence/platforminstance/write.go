package platforminstance

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/service/platforminstance"

	"retrom/internal/persistence/recordstore"
)

func (writer records) Insert(ctx context.Context, directory platforminstance.NewDirectory) error {
	input := directory.Input
	_, err := recordstore.CreatePlatformInstances(ctx, writer.database, `
INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,description,sort_order,enabled,version,
 created_at_ms,updated_at_ms,catalog_template_key
) VALUES(?,?,?,?,?,?,?,1,1,?,?,?)
`, directory.ID, input.PlatformID, input.DefaultCoreID, input.Name, directory.Slug, input.Description,
		input.SortOrder, directory.CreatedAtMS, directory.CreatedAtMS, nullableCatalogKey(directory.CatalogKey))
	if err != nil {
		return fmt.Errorf("platforminstance: insert: %w", err)
	}
	return nil
}

func (writer records) RecordCreation(ctx context.Context, audit platforminstance.CreationAudit) error {
	directory := audit.Directory
	input := directory.Input
	after, err := json.Marshal(map[string]any{
		"platformId": input.PlatformID, "defaultCoreId": input.DefaultCoreID, "name": input.Name,
		"slug": directory.Slug, "description": input.Description, "sortOrder": input.SortOrder,
		"catalogTemplateKey": nullableCatalogKey(directory.CatalogKey),
	})
	if err != nil {
		return fmt.Errorf("platforminstance: encode audit: %w", err)
	}
	return writer.RecordAudit(ctx, platforminstance.AuditEvent{
		ID: audit.ID, Action: audit.Action, ResourceType: "PLATFORM_INSTANCE", ResourceID: directory.ID,
		Actor: audit.Actor, After: json.RawMessage(after), CreatedAtMS: directory.CreatedAtMS,
	})
}

func (writer records) Update(ctx context.Context, input platforminstance.DirectoryUpdate) (bool, error) {
	result, err := recordstore.UpdatePlatformInstances(ctx, writer.database, recordstore.Update{
		Set: `
name=?,
description=?,
sort_order=?,
enabled=?,
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{Where: `
id=?
AND version=?
AND deleted_at_ms IS NULL
`, Args: []any{input.ID, input.ExpectedVersion}},
		Values: []any{input.Name, input.Description, input.SortOrder, input.Enabled, input.UpdatedAtMS},
	})
	if err != nil {
		return false, fmt.Errorf("platforminstance: update: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("platforminstance: update affected rows: %w", err)
	}
	return changed == 1, nil
}

func (writer records) Delete(ctx context.Context, input platforminstance.DirectoryDelete) (bool, error) {
	result, err := recordstore.UpdatePlatformInstances(ctx, writer.database, recordstore.Update{
		Set: `
enabled=0,
deleted_at_ms=?,
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{Where: `
id=?
AND version=?
AND deleted_at_ms IS NULL
`, Args: []any{input.ID, input.ExpectedVersion}},
		Values: []any{input.UpdatedAtMS, input.UpdatedAtMS},
	})
	if err != nil {
		return false, fmt.Errorf("platforminstance: delete: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("platforminstance: delete affected rows: %w", err)
	}
	return changed == 1, nil
}

func (writer records) ChangeDefaultCore(ctx context.Context, input platforminstance.DefaultCoreChange) (bool, error) {
	result, err := recordstore.UpdatePlatformInstances(ctx, writer.database, recordstore.Update{
		Set: `
default_core_id=?,
version=version+1,
updated_at_ms=?
`,
		Scope: recordstore.Scope{Where: `
id=?
AND version=?
`, Args: []any{input.ID, input.ExpectedVersion}},
		Values: []any{input.CoreID, input.UpdatedAtMS},
	})
	if err != nil {
		return false, fmt.Errorf("platforminstance: change default core: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("platforminstance: change default core affected rows: %w", err)
	}
	return changed == 1, nil
}

func (writer records) RecordAudit(ctx context.Context, audit platforminstance.AuditEvent) error {
	before, err := nullableJSON(audit.Before)
	if err != nil {
		return fmt.Errorf("platforminstance: encode audit before: %w", err)
	}
	after, err := nullableJSON(audit.After)
	if err != nil {
		return fmt.Errorf("platforminstance: encode audit after: %w", err)
	}
	_, err = writer.database.ExecContext(ctx, `
INSERT INTO audit_events(
 id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,'{}',?,?)
`, audit.ID, audit.Actor.Kind, audit.Actor.UserID, audit.Actor.Label, audit.Action, audit.ResourceType,
		audit.ResourceID, before, after, audit.Actor.RequestID, audit.CreatedAtMS)
	if err != nil {
		return fmt.Errorf("platforminstance: insert audit: %w", err)
	}
	return nil
}

func nullableJSON(value any) (any, error) {
	if value == nil {
		return nil, nil //nolint:nilnil // nil stores a SQL NULL audit projection.
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal platform audit JSON: %w", err)
	}
	return string(encoded), nil
}

func nullableCatalogKey(value string) any {
	if value == "" {
		return nil
	}
	return value
}
