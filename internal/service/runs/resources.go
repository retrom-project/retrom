package runs

import (
	"context"
	"sort"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

func (s *Service) resources(ctx context.Context, run *Context, files []model.GameFile,
	prepared runtimeclient.Prepared,
) ([]map[string]any, error) {
	byKey := make(map[string]model.GameFile, len(files))
	for _, file := range files {
		byKey[file.LogicalKey] = file
	}
	result := make([]map[string]any, 0, len(prepared.Resources))
	for _, plan := range prepared.Resources {
		selected, err := selectFiles(byKey, plan.Files)
		if err != nil {
			return nil, err
		}
		run.Files = append(run.Files, selected...)
		resource := map[string]any{"role": plan.Role, "kind": plan.Kind, "ordinal": plan.Ordinal}
		if err = s.resourcePlan(ctx, run, resource, selected, plan, prepared); err != nil {
			return nil, err
		}
		result = append(result, resource)
	}
	bios, err := s.bios(ctx, run, prepared, files)
	if err != nil {
		return nil, err
	}
	return append(result, bios...), nil
}

func selectFiles(byKey map[string]model.GameFile, keys []string) ([]Blob, error) {
	selected := make([]Blob, 0, len(keys))
	for _, key := range keys {
		file, exists := byKey[key]
		if !exists {
			return nil, model.ErrInvalid
		}
		selected = append(selected, blob(file))
	}
	return selected, nil
}

func (s *Service) resourcePlan(ctx context.Context, run *Context, resource map[string]any, selected []Blob,
	plan runtimeclient.Resource, prepared runtimeclient.Prepared,
) error {
	switch plan.Kind {
	case "ROM_BLOB", "SEEKABLE_BLOB", "PARENT_ARCHIVE", "WASM4_CART":
		return s.blobResource(ctx, run, resource, selected, plan)
	case "FILE_TREE", "NATIVE_WEB", "ISOLATED_WEB":
		return s.treeResource(run, resource, selected, plan, prepared)
	case "EXTERNAL_FILE_SET":
		resource["files"] = externalFiles(run.Run.ID, selected, plan.Paths)
		return nil
	default:
		return model.ErrInvalid
	}
}

func (s *Service) blobResource(ctx context.Context, run *Context, resource map[string]any, selected []Blob,
	plan runtimeclient.Resource,
) error {
	if plan.Encoding == "ZIP" || plan.Kind == "PARENT_ARCHIVE" && len(selected) > 1 {
		assembled, err := s.assembled(ctx, run, selected, plan)
		if err != nil {
			return err
		}
		selected = []Blob{assembled}
		run.Files = append(run.Files, assembled)
	}
	if len(selected) != 1 || selected[0].SizeBytes < 1 {
		return model.ErrInvalid
	}
	file := selected[0]
	resource["url"] = ResourceURL(run.Run.ID, file)
	resource["sha256"] = file.SHA256
	resource["sizeBytes"] = file.SizeBytes
	resource["rangeRequired"] = plan.Kind == "SEEKABLE_BLOB" || plan.Kind == "PARENT_ARCHIVE"
	return nil
}

func (s *Service) treeResource(run *Context, resource map[string]any, selected []Blob,
	plan runtimeclient.Resource, prepared runtimeclient.Prepared,
) error {
	index := uuid.NewString()
	for i := range selected {
		if mapped := plan.Paths[selected[i].LogicalPath]; mapped != "" {
			selected[i].LogicalPath = mapped
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].LogicalPath < selected[j].LogicalPath })
	run.Indexes[index] = selected
	resource["indexUrl"] = "/api/v1/runs/" + run.Run.ID + "/resources/index/" + index
	resource["contentDigest"] = prepared.ROMHash
	if plan.Kind == "FILE_TREE" {
		return nil
	}
	if !storage.SafeRelative(plan.BridgeAssetPath) {
		return model.ErrInvalid
	}
	provider, exists := s.Runtime.Providers[prepared.ProviderID]
	if !exists || !s.Runtime.AssetAllowed(provider.ProviderID, plan.BridgeAssetPath) {
		return model.ErrInvalid
	}
	resource["bridgeUrl"] = "/runtime/providers/" + provider.ProviderID + "/" +
		provider.BundleSHA256 + "/" + plan.BridgeAssetPath
	origin := s.isolation(run.Run.ID)
	resource["origin"] = origin
	resource["entryUrl"] = origin + "/run/" + run.Run.ID + "/" + logicalURLPath(plan.EntryFile)
	resource["entryFile"] = plan.EntryFile
	return nil
}

func externalFiles(runID string, selected []Blob, paths map[string]string) []map[string]any {
	sort.Slice(selected, func(i, j int) bool {
		return virtualPath(selected[i], paths) < virtualPath(selected[j], paths)
	})
	result := make([]map[string]any, 0, len(selected))
	for _, file := range selected {
		result = append(result, map[string]any{
			"logicalName": file.LogicalPath, "virtualPath": virtualPath(file, paths),
			"url": ResourceURL(runID, file), "sha256": file.SHA256, "sizeBytes": file.SizeBytes,
		})
	}
	return result
}

func virtualPath(file Blob, paths map[string]string) string {
	if mapped := paths[file.LogicalPath]; mapped != "" {
		return mapped
	}
	return file.LogicalPath
}
