package runs

import (
	"context"
	"errors"
	"path"
	"sort"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"

	"github.com/google/uuid"
)

func (s *Service) bios(ctx context.Context,
	run *Context,
	prepared runtimeclient.Prepared,
	_ []model.GameFile) ([]map[string]any,
	error,
) {
	requirements := prepared.BiosRequirements
	seen := make(map[string]bool, len(requirements))
	groups := make(map[string][]map[string]any)
	for _, requirement := range requirements {
		if seen[requirement.RequirementKey] {
			continue
		}
		seen[requirement.RequirementKey] = true
		file, readErr := s.Repository.Bios(ctx, requirement.RequirementKey)
		if errors.Is(readErr, model.ErrNotFound) {
			if requirement.Required {
				return nil, model.ErrBIOSMissing
			}
			continue
		}
		if readErr != nil {
			return nil, wrap(readErr)
		}
		entry := Blob{
			ID:          uuid.NewString(),
			Filename:    path.Base(requirement.LogicalName),
			Key:         file.StorageKey,
			LogicalPath: requirement.LogicalName,
			SHA256:      file.SHA256,
			SizeBytes:   file.SizeBytes,
			MediaType:   "application/octet-stream",
		}
		run.Files = append(run.Files, entry)
		virtual := requirement.LogicalName
		if requirement.VirtualPath != nil {
			virtual = *requirement.VirtualPath
		}
		groups[requirement.Delivery] = append(groups[requirement.Delivery],
			map[string]any{
				"logicalName": requirement.LogicalName,
				"virtualPath": virtual,
				"url":         ResourceURL(run.Run.ID, entry),
				"sha256":      file.SHA256,
				"sizeBytes":   file.SizeBytes,
			})
	}
	result := make([]map[string]any, 0, len(groups))
	for _, kind := range []string{"BIOS_BUNDLE", "EXTERNAL_FILE"} {
		if files, exists := groups[kind]; exists {
			sort.Slice(files, func(i, j int) bool {
				left, leftOK := files[i]["virtualPath"].(string)
				right, rightOK := files[j]["virtualPath"].(string)
				return leftOK && rightOK && left < right
			})
			resourceKind, role := kind, "bios"
			if kind == "EXTERNAL_FILE" {
				resourceKind, role = "EXTERNAL_FILE_SET", "external"
			}
			result = append(result, map[string]any{"role": role, "kind": resourceKind, "ordinal": 0, "files": files})
		}
	}
	return result, nil
}
