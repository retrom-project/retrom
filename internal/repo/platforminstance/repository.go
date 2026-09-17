package platforminstance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/platforminstance"
	"retrom/internal/repo/dbexec"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/runtime/platformcatalog"
)

type (
	Repository struct{ database *sql.DB }
	records    struct{ database dbexec.Executor }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }

func (repository *Repository) beginRead(ctx context.Context) (*sql.Tx, records, error) {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, records{}, fmt.Errorf("platforminstance: begin read: %w", err)
	}
	return transaction, records{transaction}, nil
}

func (repository *Repository) beginImmediate(ctx context.Context) (*sql.Conn, records, func(), error) {
	connection, err := repository.database.Conn(ctx)
	if err != nil {
		return nil, records{}, nil, fmt.Errorf("platforminstance: acquire connection: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = connection.Close()
		return nil, records{}, nil, fmt.Errorf("platforminstance: begin immediate: %w", err)
	}
	committed := false
	cleanup := func() {
		if !committed {
			_, _ = connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
		_ = connection.Close()
	}
	return connection, records{connection}, cleanup, nil
}

func (repository *Repository) LoadCatalogReferences(
	ctx context.Context, catalog platformcatalog.Catalog,
) (map[string]model.CatalogReference, error) {
	transaction, reader, err := repository.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	refs, err := reader.CatalogReferences(ctx, catalog)
	if err != nil {
		return nil, err
	}
	_ = transaction.Commit()
	return refs, nil
}

func (repository *Repository) LoadDirectories(ctx context.Context) ([]model.Directory, error) {
	transaction, reader, err := repository.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	dirs, err := reader.Directories(ctx)
	if err != nil {
		return nil, err
	}
	_ = transaction.Commit()
	return dirs, nil
}

func (repository *Repository) LoadInstance(ctx context.Context, id string) (model.Instance, error) {
	transaction, reader, err := repository.beginRead(ctx)
	if err != nil {
		return model.Instance{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	instance, err := reader.Instance(ctx, id)
	if err != nil {
		return model.Instance{}, err
	}
	_ = transaction.Commit()
	return instance, nil
}

func (repository *Repository) LoadCoreImpactFacts(
	ctx context.Context, instanceID, coreID string, expected int64,
) (model.CoreImpactFacts, error) {
	transaction, reader, err := repository.beginRead(ctx)
	if err != nil {
		return model.CoreImpactFacts{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	facts, err := reader.CoreImpact(ctx, instanceID, coreID, expected)
	if err != nil {
		return model.CoreImpactFacts{}, err
	}
	_ = transaction.Commit()
	return facts, nil
}

func (repository *Repository) CommitCreate(
	ctx context.Context, cmd model.CreateCommand,
) (model.Instance, error) {
	connection, bound, cleanup, err := repository.beginImmediate(ctx)
	if err != nil {
		return model.Instance{}, err
	}
	defer cleanup()

	enabled, err := bound.CoreEnabled(ctx, cmd.Input.PlatformID, cmd.Input.DefaultCoreID)
	if err != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: validate default core: %w", err)
	}
	if !enabled {
		return model.Instance{}, model.ErrDefaultCoreInvalid
	}
	base := model.SlugBase(cmd.Input.Name, cmd.Input.PlatformID)
	slugs, err := bound.UsedSlugs(ctx, cmd.Input.PlatformID, base)
	if err != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: read slugs: %w", err)
	}
	slug, err := model.NextSlug(base, slugs)
	if err != nil {
		return model.Instance{}, err
	}
	directory := model.NewDirectory{ID: cmd.ID, Slug: slug, CatalogKey: cmd.CatalogKey, Input: cmd.Input, CreatedAtMS: cmd.NowMS}
	if err := bound.Insert(ctx, directory); err != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: insert directory: %w", err)
	}
	if err := bound.RecordCreation(ctx, model.CreationAudit{
		ID: cmd.AuditID, Action: cmd.Action, Actor: cmd.Actor, Directory: directory,
	}); err != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: record creation: %w", err)
	}
	result, err := bound.Instance(ctx, cmd.ID)
	if err != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: read created directory: %w", err)
	}
	result.SupportedExtensions = contentprofile.SupportedExtensions(result.PlatformID)
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: commit: %w", err)
	}
	return result, nil
}

func (repository *Repository) CommitPatch(
	ctx context.Context, cmd model.PatchCommand,
) (model.PatchResult, error) {
	connection, bound, cleanup, err := repository.beginImmediate(ctx)
	if err != nil {
		return model.PatchResult{}, err
	}
	defer cleanup()

	current, err := bound.Instance(ctx, cmd.ID)
	if err != nil {
		return model.PatchResult{}, fmt.Errorf("platforminstance: read instance: %w", err)
	}
	if current.Version != cmd.ExpectedVersion {
		return model.PatchResult{}, model.ErrVersionConflict
	}
	if cmd.Name != nil {
		current.Name = *cmd.Name
	}
	if cmd.Description != nil {
		current.Description = *cmd.Description
	}
	if cmd.SortOrder != nil {
		current.SortOrder = *cmd.SortOrder
	}
	if cmd.Enabled != nil {
		current.Enabled = *cmd.Enabled
	}
	changed, err := bound.Update(ctx, model.DirectoryUpdate{
		ID: cmd.ID, Name: current.Name, Description: current.Description, SortOrder: current.SortOrder,
		Enabled: current.Enabled, ExpectedVersion: cmd.ExpectedVersion, UpdatedAtMS: cmd.NowMS,
	})
	if err != nil {
		return model.PatchResult{}, fmt.Errorf("platforminstance: update instance: %w", err)
	}
	if !changed {
		return model.PatchResult{}, model.ErrVersionConflict
	}
	after := map[string]any{
		"name": current.Name, "description": current.Description, "sortOrder": current.SortOrder,
		"enabled": current.Enabled, "version": cmd.ExpectedVersion + 1,
	}
	if err := bound.RecordAudit(ctx, model.AuditEvent{
		ID: cmd.AuditID, Action: "PLATFORM_INSTANCE_UPDATED", ResourceType: "PLATFORM_INSTANCE", ResourceID: cmd.ID,
		Actor: cmd.Actor, Before: current, After: after, CreatedAtMS: cmd.NowMS,
	}); err != nil {
		return model.PatchResult{}, fmt.Errorf("platforminstance: record update audit: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return model.PatchResult{}, fmt.Errorf("platforminstance: commit: %w", err)
	}
	return model.PatchResult{
		ID: cmd.ID, Name: current.Name, Description: current.Description, SortOrder: current.SortOrder,
		Enabled: current.Enabled, Version: cmd.ExpectedVersion + 1, UpdatedAtMS: cmd.NowMS,
	}, nil
}

func (repository *Repository) CommitReorder(
	ctx context.Context, cmd model.ReorderCommand,
) ([]model.ReorderResult, error) {
	connection, bound, cleanup, err := repository.beginImmediate(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	rows, err := bound.Directories(ctx)
	if err != nil {
		return nil, fmt.Errorf("platforminstance: read directories: %w", err)
	}
	current := make(map[string]model.Directory, len(rows))
	for _, row := range rows {
		if !row.Deleted {
			current[row.ID] = row
		}
	}
	if len(current) != len(cmd.Items) {
		return nil, model.ErrOrderStale
	}
	for _, item := range cmd.Items {
		row, exists := current[item.ID]
		if !exists {
			return nil, model.ErrOrderStale
		}
		if row.Version != item.Version {
			return nil, model.ErrVersionConflict
		}
	}
	result := make([]model.ReorderResult, 0, len(cmd.Items))
	for index, item := range cmd.Items {
		sortOrder := int64(index+1) * 100
		row := current[item.ID]
		changed, err := bound.Update(ctx, model.DirectoryUpdate{
			ID: item.ID, Name: row.Name, Description: row.Description, SortOrder: sortOrder,
			Enabled: row.Enabled, ExpectedVersion: item.Version, UpdatedAtMS: cmd.NowMS,
		})
		if err != nil {
			return nil, fmt.Errorf("platforminstance: update order: %w", err)
		}
		if !changed {
			return nil, model.ErrVersionConflict
		}
		if index < len(cmd.Items) {
			auditID := fmt.Sprintf("reorder-%d", index)
			if index < len(cmd.Items) && len(cmd.Items) > 0 {
				// Use deterministic audit IDs from NowMS + index for reorder operations
				auditID = fmt.Sprintf("%s-reorder-%d", item.ID, index)
			}
			if err := bound.RecordAudit(ctx, model.AuditEvent{
				ID: auditID, Action: "PLATFORM_INSTANCE_REORDERED", ResourceType: "PLATFORM_INSTANCE", ResourceID: item.ID,
				Actor:  cmd.Actor,
				Before: map[string]any{"version": row.Version, "sortOrder": row.SortOrder},
				After:  map[string]any{"version": item.Version + 1, "sortOrder": sortOrder}, CreatedAtMS: cmd.NowMS,
			}); err != nil {
				return nil, fmt.Errorf("platforminstance: record reorder audit: %w", err)
			}
		}
		result = append(result, model.ReorderResult{
			ID: item.ID, SortOrder: sortOrder, Version: item.Version + 1, UpdatedAtMS: cmd.NowMS,
		})
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, fmt.Errorf("platforminstance: commit: %w", err)
	}
	return result, nil
}

func (repository *Repository) CommitDelete(
	ctx context.Context, cmd model.DeleteCommand,
) error {
	connection, bound, cleanup, err := repository.beginImmediate(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	current, err := bound.Instance(ctx, cmd.ID)
	if err != nil {
		return fmt.Errorf("platforminstance: read instance: %w", err)
	}
	if current.Version != cmd.ExpectedVersion {
		return model.ErrVersionConflict
	}
	if current.GameCount != 0 {
		return model.ErrNotEmpty
	}
	changed, err := bound.Delete(ctx, model.DirectoryDelete{
		ID: cmd.ID, ExpectedVersion: cmd.ExpectedVersion, UpdatedAtMS: cmd.NowMS,
	})
	if err != nil {
		return fmt.Errorf("platforminstance: delete instance: %w", err)
	}
	if !changed {
		return model.ErrVersionConflict
	}
	if err := bound.RecordAudit(ctx, model.AuditEvent{
		ID: cmd.AuditID, Action: "PLATFORM_INSTANCE_DELETED", ResourceType: "PLATFORM_INSTANCE", ResourceID: cmd.ID,
		Actor: cmd.Actor, Before: current,
		After: map[string]any{"deletedAtMs": cmd.NowMS, "version": cmd.ExpectedVersion + 1}, CreatedAtMS: cmd.NowMS,
	}); err != nil {
		return fmt.Errorf("platforminstance: record delete audit: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("platforminstance: commit: %w", err)
	}
	return nil
}

func (repository *Repository) CommitChangeDefaultCore(
	ctx context.Context, cmd model.ChangeDefaultCoreCommand,
) (model.DefaultCoreChangeResult, error) {
	connection, bound, cleanup, err := repository.beginImmediate(ctx)
	if err != nil {
		return model.DefaultCoreChangeResult{}, err
	}
	defer cleanup()

	facts, err := bound.CoreImpact(ctx, cmd.InstanceID, cmd.CoreID, cmd.Expected)
	if err != nil {
		return model.DefaultCoreChangeResult{}, fmt.Errorf("platforminstance: read core impact: %w", err)
	}
	result := model.ProjectCoreImpact(cmd.InstanceID, cmd.CoreID, facts)
	actualDigest := model.ImpactDigest(result.Impact)
	if actualDigest != cmd.Digest {
		return model.DefaultCoreChangeResult{}, model.ErrImpactStale
	}
	if result.Counts["blocked"] > 0 && !cmd.ConfirmBlocked {
		return model.DefaultCoreChangeResult{}, model.ErrDefaultCoreBlocked
	}
	changed, err := bound.ChangeDefaultCore(ctx, model.DefaultCoreChange{
		ID: cmd.InstanceID, CoreID: cmd.CoreID, ExpectedVersion: cmd.Expected, UpdatedAtMS: cmd.NowMS,
	})
	if err != nil {
		return model.DefaultCoreChangeResult{}, fmt.Errorf("platforminstance: change default core: %w", err)
	}
	if !changed {
		return model.DefaultCoreChangeResult{}, model.ErrVersionConflict
	}
	if err := bound.RecordAudit(ctx, model.AuditEvent{
		ID: cmd.AuditID, Action: "PLATFORM_DEFAULT_CORE_CHANGED",
		ResourceType: "PLATFORM_INSTANCE", ResourceID: cmd.InstanceID,
		Actor:  cmd.Actor,
		Before: map[string]any{"version": cmd.Expected},
		After:  map[string]any{"defaultCoreId": cmd.CoreID, "version": cmd.Expected + 1, "impactDigest": cmd.Digest}, CreatedAtMS: cmd.NowMS,
	}); err != nil {
		return model.DefaultCoreChangeResult{}, fmt.Errorf("platforminstance: record default core audit: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return model.DefaultCoreChangeResult{}, fmt.Errorf("platforminstance: commit: %w", err)
	}
	return model.DefaultCoreChangeResult{Version: cmd.Expected + 1, UpdatedAtMS: cmd.NowMS}, nil
}

func (repository *Repository) CommitApply(
	ctx context.Context, cmd model.ApplyCommand,
) (model.IdempotentResponse, error) {
	connection, bound, cleanup, err := repository.beginImmediate(ctx)
	if err != nil {
		return model.IdempotentResponse{}, err
	}
	defer cleanup()

	stored, found, err := bound.findIdempotency(ctx, cmd.IdempotencyKey, cmd.NowMS)
	if err != nil {
		return model.IdempotentResponse{}, fmt.Errorf("platforminstance: find idempotency: %w", err)
	}
	if found {
		if stored.digest != cmd.Digest {
			return model.IdempotentResponse{}, model.ErrIdempotencyReused
		}
		stored.response.Replayed = true
		return stored.response, nil
	}
	result, err := repository.executeApply(ctx, bound, cmd)
	if err != nil {
		return model.IdempotentResponse{}, err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return model.IdempotentResponse{}, fmt.Errorf("platforminstance: encode result: %w", err)
	}
	response := model.IdempotentResponse{
		Status: 200, Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"}, Body: append(body, '\n'),
	}
	if err := bound.saveIdempotency(ctx, cmd.IdempotencyKey, cmd.Digest, response, cmd.NowMS, cmd.ExpiresAtMS); err != nil {
		return model.IdempotentResponse{}, fmt.Errorf("platforminstance: store idempotency: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return model.IdempotentResponse{}, fmt.Errorf("platforminstance: commit: %w", err)
	}
	return response, nil
}

func (repository *Repository) executeApply(
	ctx context.Context, bound records, cmd model.ApplyCommand,
) (model.ApplyResult, error) {
	if err := platformcatalog.Validate(cmd.Catalog); err != nil {
		return model.ApplyResult{}, fmt.Errorf("%w: %w", model.ErrCatalogInvalid, err)
	}
	references, err := bound.CatalogReferences(ctx, cmd.Catalog)
	if err != nil {
		return model.ApplyResult{}, fmt.Errorf("platforminstance: apply catalog: %w", err)
	}
	rows, err := bound.Directories(ctx)
	if err != nil {
		return model.ApplyResult{}, fmt.Errorf("platforminstance: apply catalog: %w", err)
	}
	before := model.ProjectRecommendations(cmd.Catalog, references, rows)
	coveredBefore := before.Summary.ActiveCount + before.Summary.CustomizedCount + before.Summary.CoveredByEquivalentCount
	maxSortOrder := int64(0)
	for _, row := range rows {
		if !row.Deleted && row.SortOrder > maxSortOrder {
			maxSortOrder = row.SortOrder
		}
	}
	nextSortOrder := int64(100)
	if maxSortOrder > 0 {
		nextSortOrder = (maxSortOrder/100 + 1) * 100
	}
	idIndex := 0
	created := make([]model.Instance, 0, before.Summary.MissingCount)
	createdKeys := make([]string, 0, before.Summary.MissingCount)
	for _, recommendation := range before.Items {
		if recommendation.State != model.StateMissing {
			continue
		}
		if idIndex >= len(cmd.InstanceIDs) || idIndex >= len(cmd.AuditIDs) {
			return model.ApplyResult{}, fmt.Errorf("platforminstance: insufficient pre-generated IDs for apply")
		}
		template := model.CatalogTemplate(cmd.Catalog, recommendation.TemplateKey)
		createCmd := model.CreateCommand{
			Actor: cmd.Actor,
			Input: model.CreateInput{
				PlatformID: template.PlatformID, DefaultCoreID: template.DefaultCoreID,
				Name: template.Name, Description: template.Description, SortOrder: nextSortOrder,
			},
			CatalogKey: template.Key,
			Action:     "PLATFORM_INSTANCE_RECOMMENDED_CREATED",
			NowMS:      cmd.NowMS,
			ID:         cmd.InstanceIDs[idIndex],
			AuditID:    cmd.AuditIDs[idIndex],
		}
		instance, err := repository.executeCreate(ctx, bound, createCmd)
		if err != nil {
			return model.ApplyResult{}, fmt.Errorf("platforminstance: apply catalog: %w", err)
		}
		created = append(created, instance)
		createdKeys = append(createdKeys, template.Key)
		nextSortOrder += 100
		idIndex++
	}
	rows, err = bound.Directories(ctx)
	if err != nil {
		return model.ApplyResult{}, fmt.Errorf("platforminstance: apply catalog: %w", err)
	}
	after := model.ProjectRecommendations(cmd.Catalog, references, rows)
	return model.ApplyResult{
		CatalogVersion: cmd.Catalog.Version, CreatedTemplateKeys: createdKeys, Created: created,
		Summary: model.ApplySummary{
			CreatedCount: len(created), CoveredCount: coveredBefore,
			SuppressedCount: before.Summary.SuppressedCount, RemainingMissingCount: after.Summary.MissingCount,
		},
		Items: after.Items,
	}, nil
}

func (repository *Repository) executeCreate(
	ctx context.Context, bound records, cmd model.CreateCommand,
) (model.Instance, error) {
	enabled, err := bound.CoreEnabled(ctx, cmd.Input.PlatformID, cmd.Input.DefaultCoreID)
	if err != nil {
		return model.Instance{}, fmt.Errorf("validate default core: %w", err)
	}
	if !enabled {
		return model.Instance{}, model.ErrDefaultCoreInvalid
	}
	base := model.SlugBase(cmd.Input.Name, cmd.Input.PlatformID)
	slugs, err := bound.UsedSlugs(ctx, cmd.Input.PlatformID, base)
	if err != nil {
		return model.Instance{}, fmt.Errorf("read slugs: %w", err)
	}
	slug, err := model.NextSlug(base, slugs)
	if err != nil {
		return model.Instance{}, err
	}
	directory := model.NewDirectory{ID: cmd.ID, Slug: slug, CatalogKey: cmd.CatalogKey, Input: cmd.Input, CreatedAtMS: cmd.NowMS}
	if err := bound.Insert(ctx, directory); err != nil {
		return model.Instance{}, fmt.Errorf("insert directory: %w", err)
	}
	if err := bound.RecordCreation(ctx, model.CreationAudit{
		ID: cmd.AuditID, Action: cmd.Action, Actor: cmd.Actor, Directory: directory,
	}); err != nil {
		return model.Instance{}, fmt.Errorf("record creation: %w", err)
	}
	result, err := bound.Instance(ctx, cmd.ID)
	if err != nil {
		return model.Instance{}, fmt.Errorf("read created directory: %w", err)
	}
	result.SupportedExtensions = contentprofile.SupportedExtensions(result.PlatformID)
	return result, nil
}
