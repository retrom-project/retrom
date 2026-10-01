package platforminstance

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	contentcapability "retrom/internal/content/capability"
	contentprofile "retrom/internal/content/profile"
)

type PlatformInstancePatch struct {
	ID              string
	ExpectedVersion int64
	Name            *string
	Description     *string
	Enabled         *bool
	Actor           AuditActor
}

type PlatformInstancePatchResult struct {
	ID          string
	Name        string
	Description string
	Enabled     bool
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
		if input.Enabled != nil {
			current.Enabled = *input.Enabled
		}
		now := service.now().UnixMilli()
		changed, err := scope.Directories.Update(ctx, DirectoryUpdate{
			ID: input.ID, Name: current.Name, Description: current.Description,
			Enabled: current.Enabled, ExpectedVersion: input.ExpectedVersion, UpdatedAtMS: now,
		})
		if err != nil {
			return fmt.Errorf("update instance: %w", err)
		}
		if !changed {
			return ErrVersionConflict
		}
		after := map[string]any{
			"name": current.Name, "description": current.Description,
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
			ID: input.ID, Name: current.Name, Description: current.Description,
			Enabled: current.Enabled, Version: input.ExpectedVersion + 1, UpdatedAtMS: now,
		}
		return nil
	})
	return result, repositoryError("patch", err)
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

func validPatch(input PlatformInstancePatch) bool {
	hasChange := input.Name != nil || input.Description != nil || input.Enabled != nil
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
