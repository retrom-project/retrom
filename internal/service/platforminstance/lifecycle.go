package platforminstance

import (
	"context"
	"fmt"
	model "retrom/internal/model/platforminstance"

	"github.com/google/uuid"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/content/contentprofile"
)

type PlatformInstancePatch struct {
	ID              string
	ExpectedVersion int64
	Name            *string
	Description     *string
	SortOrder       *int64
	Enabled         *bool
	Actor           model.AuditActor
}

type PlatformInstancePatchResult struct {
	ID          string
	Name        string
	Description string
	SortOrder   int64
	Enabled     bool
	Version     int64
	UpdatedAtMS int64
}

type PlatformInstanceOrderItem struct {
	ID      string
	Version int64
}

type PlatformInstanceOrderResult struct {
	ID          string
	SortOrder   int64
	Version     int64
	UpdatedAtMS int64
}

type PlatformInstanceDelete struct {
	ID              string
	ExpectedVersion int64
	Actor           model.AuditActor
}

func (service *Service) Read(ctx context.Context, id string, multiDiscEnabled bool) (model.Instance, error) {
	if id == "" {
		return model.Instance{}, model.ErrInvalid
	}
	var result model.Instance
	err := service.repository.WithRead(ctx, func(reader model.Reader) error {
		var err error
		result, err = reader.Instance(ctx, id)
		if err != nil {
			return fmt.Errorf("read instance: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.Instance{}, repositoryError("read", err)
	}
	result.SupportedExtensions = supportedExtensions(result.PlatformID)
	result.ImportCapabilities = importCapabilities(result, multiDiscEnabled)
	return result, nil
}

func (service *Service) Patch(ctx context.Context, input PlatformInstancePatch) (PlatformInstancePatchResult, error) {
	if input.ID == "" || input.ExpectedVersion < 1 || !validPatch(input) {
		return PlatformInstancePatchResult{}, model.ErrInvalid
	}
	var result PlatformInstancePatchResult
	err := service.repository.CommitWrite(ctx, func(scope model.WriteScope) error {
		current, err := scope.Reader.Instance(ctx, input.ID)
		if err != nil {
			return fmt.Errorf("read instance: %w", err)
		}
		if current.Version != input.ExpectedVersion {
			return model.ErrVersionConflict
		}
		if input.Name != nil {
			current.Name = *input.Name
		}
		if input.Description != nil {
			current.Description = *input.Description
		}
		if input.SortOrder != nil {
			current.SortOrder = *input.SortOrder
		}
		if input.Enabled != nil {
			current.Enabled = *input.Enabled
		}
		now := service.now().UnixMilli()
		changed, err := scope.Directories.Update(ctx, model.DirectoryUpdate{
			ID: input.ID, Name: current.Name, Description: current.Description, SortOrder: current.SortOrder,
			Enabled: current.Enabled, ExpectedVersion: input.ExpectedVersion, UpdatedAtMS: now,
		})
		if err != nil {
			return fmt.Errorf("update instance: %w", err)
		}
		if !changed {
			return model.ErrVersionConflict
		}
		after := map[string]any{
			"name": current.Name, "description": current.Description, "sortOrder": current.SortOrder,
			"enabled": current.Enabled, "version": input.ExpectedVersion + 1,
		}
		auditID, err := newAuditID()
		if err != nil {
			return err
		}
		if err := scope.Directories.RecordAudit(ctx, model.AuditEvent{
			ID: auditID, Action: "PLATFORM_INSTANCE_UPDATED", ResourceType: "PLATFORM_INSTANCE", ResourceID: input.ID,
			Actor: input.Actor, Before: current, After: after, CreatedAtMS: now,
		}); err != nil {
			return fmt.Errorf("record update audit: %w", err)
		}
		result = PlatformInstancePatchResult{
			ID: input.ID, Name: current.Name, Description: current.Description, SortOrder: current.SortOrder,
			Enabled: current.Enabled, Version: input.ExpectedVersion + 1, UpdatedAtMS: now,
		}
		return nil
	})
	return result, repositoryError("patch", err)
}

func (service *Service) Reorder(
	ctx context.Context,
	actor model.AuditActor,
	items []PlatformInstanceOrderItem,
) ([]PlatformInstanceOrderResult, error) {
	if err := validateOrderItems(items); err != nil {
		return nil, err
	}
	var result []PlatformInstanceOrderResult
	err := service.repository.CommitWrite(ctx, func(scope model.WriteScope) error {
		rows, err := scope.Reader.Directories(ctx)
		if err != nil {
			return fmt.Errorf("read directories: %w", err)
		}
		current := activeDirectories(rows)
		if err := verifyOrder(current, items); err != nil {
			return err
		}
		result, err = service.applyOrder(ctx, scope, actor, current, items, service.now().UnixMilli())
		return err
	})
	return result, repositoryError("reorder", err)
}

func (service *Service) Delete(ctx context.Context, input PlatformInstanceDelete) error {
	if input.ID == "" || input.ExpectedVersion < 1 {
		return model.ErrInvalid
	}
	err := service.repository.CommitWrite(ctx, func(scope model.WriteScope) error {
		current, err := scope.Reader.Instance(ctx, input.ID)
		if err != nil {
			return fmt.Errorf("read instance: %w", err)
		}
		if current.Version != input.ExpectedVersion {
			return model.ErrVersionConflict
		}
		if current.GameCount != 0 {
			return model.ErrNotEmpty
		}
		now := service.now().UnixMilli()
		changed, err := scope.Directories.Delete(ctx, model.DirectoryDelete{
			ID: input.ID, ExpectedVersion: input.ExpectedVersion, UpdatedAtMS: now,
		})
		if err != nil {
			return fmt.Errorf("delete instance: %w", err)
		}
		if !changed {
			return model.ErrVersionConflict
		}
		auditID, err := newAuditID()
		if err != nil {
			return fmt.Errorf("create delete audit id: %w", err)
		}
		if err := scope.Directories.RecordAudit(ctx, model.AuditEvent{
			ID: auditID, Action: "PLATFORM_INSTANCE_DELETED", ResourceType: "PLATFORM_INSTANCE", ResourceID: input.ID,
			Actor: input.Actor, Before: current,
			After: map[string]any{"deletedAtMs": now, "version": input.ExpectedVersion + 1}, CreatedAtMS: now,
		}); err != nil {
			return fmt.Errorf("record delete audit: %w", err)
		}
		return nil
	})
	return repositoryError("delete", err)
}

func validateOrderItems(items []PlatformInstanceOrderItem) error {
	if len(items) == 0 {
		return model.ErrInvalid
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, err := uuid.Parse(item.ID); err != nil || item.Version < 1 {
			return model.ErrInvalid
		}
		if _, exists := seen[item.ID]; exists {
			return model.ErrInvalid
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

func activeDirectories(rows []model.Directory) map[string]model.Directory {
	current := make(map[string]model.Directory, len(rows))
	for _, row := range rows {
		if !row.Deleted {
			current[row.ID] = row
		}
	}
	return current
}

func verifyOrder(current map[string]model.Directory, items []PlatformInstanceOrderItem) error {
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

func (service *Service) applyOrder(
	ctx context.Context,
	scope model.WriteScope,
	actor model.AuditActor,
	current map[string]model.Directory,
	items []PlatformInstanceOrderItem,
	now int64,
) ([]PlatformInstanceOrderResult, error) {
	result := make([]PlatformInstanceOrderResult, 0, len(items))
	for index, item := range items {
		updated, err := service.updateOrderItem(
			ctx, scope, actor, current[item.ID], item, int64(index+1)*100, now,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, updated)
	}
	return result, nil
}

func (service *Service) updateOrderItem(
	ctx context.Context,
	scope model.WriteScope,
	actor model.AuditActor,
	row model.Directory,
	item PlatformInstanceOrderItem,
	sortOrder, now int64,
) (PlatformInstanceOrderResult, error) {
	changed, err := scope.Directories.Update(ctx, model.DirectoryUpdate{
		ID: item.ID, Name: row.Name, Description: row.Description, SortOrder: sortOrder,
		Enabled: row.Enabled, ExpectedVersion: item.Version, UpdatedAtMS: now,
	})
	if err != nil {
		return PlatformInstanceOrderResult{}, fmt.Errorf("update order: %w", err)
	}
	if !changed {
		return PlatformInstanceOrderResult{}, model.ErrVersionConflict
	}
	auditID, err := newAuditID()
	if err != nil {
		return PlatformInstanceOrderResult{}, fmt.Errorf("create reorder audit id: %w", err)
	}
	if err := scope.Directories.RecordAudit(ctx, model.AuditEvent{
		ID: auditID, Action: "PLATFORM_INSTANCE_REORDERED", ResourceType: "PLATFORM_INSTANCE", ResourceID: item.ID,
		Actor:  actor,
		Before: map[string]any{"version": row.Version, "sortOrder": row.SortOrder},
		After:  map[string]any{"version": item.Version + 1, "sortOrder": sortOrder}, CreatedAtMS: now,
	}); err != nil {
		return PlatformInstanceOrderResult{}, fmt.Errorf("record reorder audit: %w", err)
	}
	return PlatformInstanceOrderResult{
		ID: item.ID, SortOrder: sortOrder, Version: item.Version + 1, UpdatedAtMS: now,
	}, nil
}

func validPatch(input PlatformInstancePatch) bool {
	hasChange := input.Name != nil || input.Description != nil || input.SortOrder != nil || input.Enabled != nil
	return hasChange &&
		(input.Name == nil || validText(*input.Name, 1, 200, false)) &&
		(input.Description == nil || validText(*input.Description, 0, 10_000, true))
}

func newAuditID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("platforminstance: create audit id: %w", err)
	}
	return id.String(), nil
}

func supportedExtensions(platformID string) []string {
	return contentprofile.SupportedExtensions(platformID)
}

func importCapabilities(instance model.Instance, multiDiscEnabled bool) contentcapability.ImportCapabilities {
	return contentcapability.Resolve(instance.PlatformID, instance.Enabled, multiDiscEnabled, instance.ContentPolicy)
}
