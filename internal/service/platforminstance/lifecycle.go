package platforminstance

import (
	"context"
	"fmt"

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
	Actor           AuditActor
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
	Actor           AuditActor
}

func (service *Service) Read(ctx context.Context, id string, multiDiscEnabled bool) (Instance, error) {
	if id == "" {
		return Instance{}, ErrInvalid
	}
	var result Instance
	err := service.repository.WithRead(ctx, func(reader Reader) error {
		var err error
		result, err = reader.Instance(ctx, id)
		if err != nil {
			return fmt.Errorf("read instance: %w", err)
		}
		return nil
	})
	if err != nil {
		return Instance{}, repositoryError("read", err)
	}
	result.SupportedExtensions = supportedExtensions(result.PlatformID)
	result.ImportCapabilities = importCapabilities(result, multiDiscEnabled)
	return result, nil
}

func (service *Service) Patch(ctx context.Context, input PlatformInstancePatch) (PlatformInstancePatchResult, error) {
	if input.ID == "" || input.ExpectedVersion < 1 || !validPatch(input) {
		return PlatformInstancePatchResult{}, ErrInvalid
	}
	var result PlatformInstancePatchResult
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		current, err := scope.Reader.Instance(ctx, input.ID)
		if err != nil {
			return fmt.Errorf("read instance: %w", err)
		}
		if current.Version != input.ExpectedVersion {
			return ErrVersionConflict
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
		changed, err := scope.Directories.Update(ctx, DirectoryUpdate{
			ID: input.ID, Name: current.Name, Description: current.Description, SortOrder: current.SortOrder,
			Enabled: current.Enabled, ExpectedVersion: input.ExpectedVersion, UpdatedAtMS: now,
		})
		if err != nil {
			return fmt.Errorf("update instance: %w", err)
		}
		if !changed {
			return ErrVersionConflict
		}
		after := map[string]any{
			"name": current.Name, "description": current.Description, "sortOrder": current.SortOrder,
			"enabled": current.Enabled, "version": input.ExpectedVersion + 1,
		}
		auditID, err := newAuditID()
		if err != nil {
			return err
		}
		if err := scope.Directories.RecordAudit(ctx, AuditEvent{
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
	actor AuditActor,
	items []PlatformInstanceOrderItem,
) ([]PlatformInstanceOrderResult, error) {
	if err := validateOrderItems(items); err != nil {
		return nil, err
	}
	var result []PlatformInstanceOrderResult
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
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
		return ErrInvalid
	}
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		current, err := scope.Reader.Instance(ctx, input.ID)
		if err != nil {
			return fmt.Errorf("read instance: %w", err)
		}
		if current.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		if current.GameCount != 0 {
			return ErrNotEmpty
		}
		now := service.now().UnixMilli()
		changed, err := scope.Directories.Delete(ctx, DirectoryDelete{
			ID: input.ID, ExpectedVersion: input.ExpectedVersion, UpdatedAtMS: now,
		})
		if err != nil {
			return fmt.Errorf("delete instance: %w", err)
		}
		if !changed {
			return ErrVersionConflict
		}
		auditID, err := newAuditID()
		if err != nil {
			return fmt.Errorf("create delete audit id: %w", err)
		}
		if err := scope.Directories.RecordAudit(ctx, AuditEvent{
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
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, err := uuid.Parse(item.ID); err != nil || item.Version < 1 {
			return ErrInvalid
		}
		if _, exists := seen[item.ID]; exists {
			return ErrInvalid
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

func activeDirectories(rows []Directory) map[string]Directory {
	current := make(map[string]Directory, len(rows))
	for _, row := range rows {
		if !row.Deleted {
			current[row.ID] = row
		}
	}
	return current
}

func verifyOrder(current map[string]Directory, items []PlatformInstanceOrderItem) error {
	if len(current) != len(items) {
		return ErrOrderStale
	}
	for _, item := range items {
		row, exists := current[item.ID]
		if !exists {
			return ErrOrderStale
		}
		if row.Version != item.Version {
			return ErrVersionConflict
		}
	}
	return nil
}

func (service *Service) applyOrder(
	ctx context.Context,
	scope WriteScope,
	actor AuditActor,
	current map[string]Directory,
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
	scope WriteScope,
	actor AuditActor,
	row Directory,
	item PlatformInstanceOrderItem,
	sortOrder, now int64,
) (PlatformInstanceOrderResult, error) {
	changed, err := scope.Directories.Update(ctx, DirectoryUpdate{
		ID: item.ID, Name: row.Name, Description: row.Description, SortOrder: sortOrder,
		Enabled: row.Enabled, ExpectedVersion: item.Version, UpdatedAtMS: now,
	})
	if err != nil {
		return PlatformInstanceOrderResult{}, fmt.Errorf("update order: %w", err)
	}
	if !changed {
		return PlatformInstanceOrderResult{}, ErrVersionConflict
	}
	auditID, err := newAuditID()
	if err != nil {
		return PlatformInstanceOrderResult{}, fmt.Errorf("create reorder audit id: %w", err)
	}
	if err := scope.Directories.RecordAudit(ctx, AuditEvent{
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

func importCapabilities(instance Instance, multiDiscEnabled bool) contentcapability.ImportCapabilities {
	return contentcapability.Resolve(instance.PlatformID, instance.Enabled, multiDiscEnabled, instance.ContentPolicy)
}
