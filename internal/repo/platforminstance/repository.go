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
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, records{}, fmt.Errorf("platforminstance: begin read: %w", err)
	}
	return transaction, records{transaction}, nil
}

func (repository *Repository) LoadCatalogReferences(
	ctx context.Context, catalog platformcatalog.Catalog,
) (map[string]model.CatalogReference, error) {
	transaction, reader, err := repository.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	refs, readErr := reader.CatalogReferences(ctx, catalog)
	if readErr != nil {
		return nil, readErr
	}
	if commitErr := transaction.Commit(); commitErr != nil {
		return nil, fmt.Errorf("platforminstance: commit read: %w", commitErr)
	}
	return refs, nil
}

func (repository *Repository) LoadDirectories(ctx context.Context) ([]model.Directory, error) {
	transaction, reader, err := repository.beginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()
	dirs, readErr := reader.Directories(ctx)
	if readErr != nil {
		return nil, readErr
	}
	if commitErr := transaction.Commit(); commitErr != nil {
		return nil, fmt.Errorf("platforminstance: commit read: %w", commitErr)
	}
	return dirs, nil
}

func (repository *Repository) LoadInstance(ctx context.Context, id string) (model.Instance, error) {
	transaction, reader, err := repository.beginRead(ctx)
	if err != nil {
		return model.Instance{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	instance, readErr := reader.Instance(ctx, id)
	if readErr != nil {
		return model.Instance{}, readErr
	}
	if commitErr := transaction.Commit(); commitErr != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: commit read: %w", commitErr)
	}
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
	facts, readErr := reader.CoreImpact(ctx, instanceID, coreID, expected)
	if readErr != nil {
		return model.CoreImpactFacts{}, readErr
	}
	if commitErr := transaction.Commit(); commitErr != nil {
		return model.CoreImpactFacts{}, fmt.Errorf(
			"platforminstance: commit read: %w", commitErr,
		)
	}
	return facts, nil
}

func (repository *Repository) CommitCreate(
	ctx context.Context, cmd model.CreateCommand,
) (model.Instance, error) {
	var result model.Instance
	err := dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		bound := records{db}
		instance, createErr := repository.executeCreate(ctx, bound, cmd)
		if createErr != nil {
			return createErr
		}
		result = instance
		return nil
	})
	return result, err
}

func (repository *Repository) CommitPatch(
	ctx context.Context, cmd model.PatchCommand,
) (model.PatchResult, error) {
	var result model.PatchResult
	err := dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		bound := records{db}
		current, readErr := bound.Instance(ctx, cmd.ID)
		if readErr != nil {
			return fmt.Errorf("platforminstance: read instance: %w", readErr)
		}
		if current.Version != cmd.ExpectedVersion {
			return model.ErrVersionConflict
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
		changed, updateErr := bound.Update(ctx, model.DirectoryUpdate{
			ID: cmd.ID, Name: current.Name, Description: current.Description,
			SortOrder: current.SortOrder, Enabled: current.Enabled,
			ExpectedVersion: cmd.ExpectedVersion, UpdatedAtMS: cmd.NowMS,
		})
		if updateErr != nil {
			return fmt.Errorf("platforminstance: update instance: %w", updateErr)
		}
		if !changed {
			return model.ErrVersionConflict
		}
		after := map[string]any{
			"name": current.Name, "description": current.Description,
			"sortOrder": current.SortOrder,
			"enabled": current.Enabled, "version": cmd.ExpectedVersion + 1,
		}
		if auditErr := bound.RecordAudit(ctx, model.AuditEvent{
			ID: cmd.AuditID, Action: "PLATFORM_INSTANCE_UPDATED",
			ResourceType: "PLATFORM_INSTANCE", ResourceID: cmd.ID,
			Actor: cmd.Actor, Before: current, After: after,
			CreatedAtMS: cmd.NowMS,
		}); auditErr != nil {
			return fmt.Errorf("platforminstance: record update audit: %w", auditErr)
		}
		result = model.PatchResult{
			ID: cmd.ID, Name: current.Name,
			Description: current.Description, SortOrder: current.SortOrder,
			Enabled: current.Enabled, Version: cmd.ExpectedVersion + 1,
			UpdatedAtMS: cmd.NowMS,
		}
		return nil
	})
	return result, err
}

func (repository *Repository) CommitReorder(
	ctx context.Context, cmd model.ReorderCommand,
) ([]model.ReorderResult, error) {
	var result []model.ReorderResult
	err := dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		bound := records{db}
		current, loadErr := loadActiveDirectories(ctx, bound)
		if loadErr != nil {
			return loadErr
		}
		if validateErr := validateReorderItems(current, cmd.Items); validateErr != nil {
			return validateErr
		}
		reordered, applyErr := applyReorder(ctx, bound, cmd, current)
		if applyErr != nil {
			return applyErr
		}
		result = reordered
		return nil
	})
	return result, err
}

func loadActiveDirectories(
	ctx context.Context, bound records,
) (map[string]model.Directory, error) {
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
	return current, nil
}

func validateReorderItems(
	current map[string]model.Directory,
	items []model.ReorderItem,
) error {
	if len(current) != len(items) {
		return model.ErrOrderStale
	}
	for _, item := range items {
		row, exists := current[item.ID]
		if !exists {
			return model.ErrOrderStale
		}
		if row.Version != item.Version {
			return model.ErrVersionConflict
		}
	}
	return nil
}

func applyReorder(
	ctx context.Context, bound records,
	cmd model.ReorderCommand,
	current map[string]model.Directory,
) ([]model.ReorderResult, error) {
	result := make([]model.ReorderResult, 0, len(cmd.Items))
	for index, item := range cmd.Items {
		sortOrder := int64(index+1) * 100
		row := current[item.ID]
		changed, err := bound.Update(ctx, model.DirectoryUpdate{
			ID: item.ID, Name: row.Name,
			Description: row.Description, SortOrder: sortOrder,
			Enabled: row.Enabled, ExpectedVersion: item.Version,
			UpdatedAtMS: cmd.NowMS,
		})
		if err != nil {
			return nil, fmt.Errorf("platforminstance: update order: %w", err)
		}
		if !changed {
			return nil, model.ErrVersionConflict
		}
		auditID := fmt.Sprintf("%s-reorder-%d", item.ID, index)
		if err := bound.RecordAudit(ctx, model.AuditEvent{
			ID: auditID, Action: "PLATFORM_INSTANCE_REORDERED",
			ResourceType: "PLATFORM_INSTANCE", ResourceID: item.ID,
			Actor: cmd.Actor,
			Before: map[string]any{
				"version": row.Version, "sortOrder": row.SortOrder,
			},
			After: map[string]any{
				"version":   item.Version + 1,
				"sortOrder": sortOrder,
			},
			CreatedAtMS: cmd.NowMS,
		}); err != nil {
			return nil, fmt.Errorf("platforminstance: record reorder audit: %w", err)
		}
		result = append(result, model.ReorderResult{
			ID: item.ID, SortOrder: sortOrder,
			Version: item.Version + 1, UpdatedAtMS: cmd.NowMS,
		})
	}
	return result, nil
}

func (repository *Repository) CommitDelete(
	ctx context.Context, cmd model.DeleteCommand,
) error {
	return dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		bound := records{db}
		current, readErr := bound.Instance(ctx, cmd.ID)
		if readErr != nil {
			return fmt.Errorf("platforminstance: read instance: %w", readErr)
		}
		if current.Version != cmd.ExpectedVersion {
			return model.ErrVersionConflict
		}
		if current.GameCount != 0 {
			return model.ErrNotEmpty
		}
		changed, deleteErr := bound.Delete(ctx, model.DirectoryDelete{
			ID: cmd.ID, ExpectedVersion: cmd.ExpectedVersion, UpdatedAtMS: cmd.NowMS,
		})
		if deleteErr != nil {
			return fmt.Errorf("platforminstance: delete instance: %w", deleteErr)
		}
		if !changed {
			return model.ErrVersionConflict
		}
		return bound.RecordAudit(ctx, model.AuditEvent{
			ID: cmd.AuditID, Action: "PLATFORM_INSTANCE_DELETED",
			ResourceType: "PLATFORM_INSTANCE", ResourceID: cmd.ID,
			Actor: cmd.Actor, Before: current,
			After: map[string]any{
				"deletedAtMs": cmd.NowMS,
				"version":     cmd.ExpectedVersion + 1,
			},
			CreatedAtMS: cmd.NowMS,
		})
	})
}

func (repository *Repository) CommitChangeDefaultCore(
	ctx context.Context, cmd model.ChangeDefaultCoreCommand,
) (model.DefaultCoreChangeResult, error) {
	var result model.DefaultCoreChangeResult
	err := dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		bound := records{db}
		facts, readErr := bound.CoreImpact(ctx, cmd.InstanceID, cmd.CoreID, cmd.Expected)
		if readErr != nil {
			return fmt.Errorf("platforminstance: read core impact: %w", readErr)
		}
		projected := model.ProjectCoreImpact(cmd.InstanceID, cmd.CoreID, facts)
		actualDigest := model.ImpactDigest(projected.Impact)
		if actualDigest != cmd.Digest {
			return model.ErrImpactStale
		}
		if projected.Counts["blocked"] > 0 && !cmd.ConfirmBlocked {
			return model.ErrDefaultCoreBlocked
		}
		changed, changeErr := bound.ChangeDefaultCore(ctx, model.DefaultCoreChange{
			ID: cmd.InstanceID, CoreID: cmd.CoreID,
			ExpectedVersion: cmd.Expected, UpdatedAtMS: cmd.NowMS,
		})
		if changeErr != nil {
			return fmt.Errorf(
				"platforminstance: change default core: %w", changeErr,
			)
		}
		if !changed {
			return model.ErrVersionConflict
		}
		if auditErr := bound.RecordAudit(ctx, model.AuditEvent{
			ID: cmd.AuditID, Action: "PLATFORM_DEFAULT_CORE_CHANGED",
			ResourceType: "PLATFORM_INSTANCE", ResourceID: cmd.InstanceID,
			Actor:  cmd.Actor,
			Before: map[string]any{"version": cmd.Expected},
			After: map[string]any{
				"defaultCoreId": cmd.CoreID,
				"version":       cmd.Expected + 1,
				"impactDigest":  cmd.Digest,
			},
			CreatedAtMS: cmd.NowMS,
		}); auditErr != nil {
			return fmt.Errorf(
				"platforminstance: record default core audit: %w", auditErr,
			)
		}
		result = model.DefaultCoreChangeResult{
			Version: cmd.Expected + 1, UpdatedAtMS: cmd.NowMS,
		}
		return nil
	})
	return result, err
}

func (repository *Repository) CommitApply(
	ctx context.Context, cmd model.ApplyCommand,
) (model.IdempotentResponse, error) {
	var response model.IdempotentResponse
	err := dbexec.Immediate(ctx, repository.database, func(db dbexec.Executor) error {
		bound := records{db}
		stored, found, findErr := bound.findIdempotency(ctx, cmd.IdempotencyKey, cmd.NowMS)
		if findErr != nil {
			return fmt.Errorf("platforminstance: find idempotency: %w", findErr)
		}
		if found {
			if stored.digest != cmd.Digest {
				return model.ErrIdempotencyReused
			}
			stored.response.Replayed = true
			response = stored.response
			return nil
		}
		result, applyErr := repository.executeApply(ctx, bound, cmd)
		if applyErr != nil {
			return applyErr
		}
		body, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			return fmt.Errorf("platforminstance: encode result: %w", encodeErr)
		}
		response = model.IdempotentResponse{
			Status:  200,
			Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
			Body:    append(body, '\n'),
		}
		return bound.saveIdempotency(
			ctx, cmd.IdempotencyKey, cmd.Digest,
			response, cmd.NowMS, cmd.ExpiresAtMS,
		)
	})
	return response, err
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
			return model.ApplyResult{}, fmt.Errorf("platforminstance: %w", model.ErrInsufficientIDs)
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
		return model.Instance{}, fmt.Errorf("apply: next slug: %w", err)
	}
	directory := model.NewDirectory{
		ID: cmd.ID, Slug: slug, CatalogKey: cmd.CatalogKey,
		Input: cmd.Input, CreatedAtMS: cmd.NowMS,
	}
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
