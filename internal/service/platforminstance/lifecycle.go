package platforminstance

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

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

type PlatformInstanceOrderItem struct {
	ID      string
	Version int64
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
	result, err := service.repository.LoadInstance(ctx, id)
	if err != nil {
		return model.Instance{}, repositoryError("read", err)
	}
	result.SupportedExtensions = supportedExtensions(result.PlatformID)
	result.ImportCapabilities = importCapabilities(result, multiDiscEnabled)
	return result, nil
}

func (service *Service) Patch(ctx context.Context, input PlatformInstancePatch) (model.PatchResult, error) {
	if input.ID == "" || input.ExpectedVersion < 1 || !validPatch(input) {
		return model.PatchResult{}, model.ErrInvalid
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return model.PatchResult{}, fmt.Errorf("platforminstance: create audit id: %w", err)
	}
	result, err := service.repository.CommitPatch(ctx, model.PatchCommand{
		ID: input.ID, ExpectedVersion: input.ExpectedVersion,
		Name: input.Name, Description: input.Description,
		SortOrder: input.SortOrder, Enabled: input.Enabled,
		Actor: input.Actor, NowMS: service.now().UnixMilli(), AuditID: auditID.String(),
	})
	return result, repositoryError("patch", err)
}

func (service *Service) Reorder(
	ctx context.Context,
	actor model.AuditActor,
	items []PlatformInstanceOrderItem,
) ([]model.ReorderResult, error) {
	if err := validateOrderItems(items); err != nil {
		return nil, err
	}
	reorderItems := make([]model.ReorderItem, len(items))
	for i, item := range items {
		reorderItems[i] = model.ReorderItem{ID: item.ID, Version: item.Version}
	}
	result, err := service.repository.CommitReorder(ctx, model.ReorderCommand{
		Actor: actor, Items: reorderItems, NowMS: service.now().UnixMilli(),
	})
	return result, repositoryError("reorder", err)
}

func (service *Service) Delete(ctx context.Context, input PlatformInstanceDelete) error {
	if input.ID == "" || input.ExpectedVersion < 1 {
		return model.ErrInvalid
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("platforminstance: create delete audit id: %w", err)
	}
	err = service.repository.CommitDelete(ctx, model.DeleteCommand{
		ID: input.ID, ExpectedVersion: input.ExpectedVersion,
		Actor: input.Actor, NowMS: service.now().UnixMilli(), AuditID: auditID.String(),
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

func validPatch(input PlatformInstancePatch) bool {
	hasChange := input.Name != nil || input.Description != nil || input.SortOrder != nil || input.Enabled != nil
	return hasChange &&
		(input.Name == nil || validText(*input.Name, 1, 200, false)) &&
		(input.Description == nil || validText(*input.Description, 0, 10_000, true))
}

func validText(value string, minimum, maximum int, allowNewline bool) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) {
		return false
	}
	count := 0
	for _, character := range value {
		if unicode.IsControl(character) &&
			(!allowNewline || character != '\n' && character != '\r' && character != '\t') {
			return false
		}
		count++
	}
	return count >= minimum && count <= maximum
}

func supportedExtensions(platformID string) []string {
	return contentprofile.SupportedExtensions(platformID)
}

func importCapabilities(instance model.Instance, multiDiscEnabled bool) contentcapability.ImportCapabilities {
	return contentcapability.Resolve(instance.PlatformID, instance.Enabled, multiDiscEnabled, instance.ContentPolicy)
}
