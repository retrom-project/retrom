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
	_, err = writer.database.ExecContext(ctx, `
INSERT INTO audit_events(
 id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
 before_json,after_json,diff_json,request_id,created_at_ms
) VALUES(?,?,?,?,?,'PLATFORM_INSTANCE',?,NULL,?,'{}',?,?)
`, audit.ID, audit.Actor.Kind, audit.Actor.UserID, audit.Actor.Label, audit.Action, directory.ID,
		string(after), audit.Actor.RequestID, directory.CreatedAtMS)
	if err != nil {
		return fmt.Errorf("platforminstance: insert audit: %w", err)
	}
	return nil
}

func nullableCatalogKey(value string) any {
	if value == "" {
		return nil
	}
	return value
}
