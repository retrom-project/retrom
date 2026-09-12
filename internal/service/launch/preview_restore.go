package launch

import (
	"context"
	"fmt"
	"slices"
)

func readPreviewRestore(
	ctx context.Context,
	scope PreviewCreationScope,
	request ReviewPreviewRequest,
) (PreviewRestore, error) {
	if request.RestoreFromPreviewID == nil {
		return PreviewRestore{}, nil
	}
	restore, found, err := scope.Restore(ctx, *request.RestoreFromPreviewID)
	if err != nil {
		return PreviewRestore{}, fmt.Errorf("read preview restore: %w", err)
	}
	if !found {
		return PreviewRestore{}, ErrSaveIncompatible
	}
	return restore, nil
}

func validatePreviewRestore(restore PreviewRestore, plan PreviewCreatePlan) error {
	if !previewRestoreOwner(restore, plan) || !previewRestoreContent(restore, plan) ||
		restore.SizeBytes <= 0 || restore.SizeBytes > restore.MaximumBytes ||
		!slices.Contains(restore.ReadFormats, restore.Format) || !samePreviewFiles(restore.Files, plan.Content.Files) {
		return ErrSaveIncompatible
	}
	return nil
}

func previewRestoreOwner(restore PreviewRestore, plan PreviewCreatePlan) bool {
	return restore.ActorID == plan.Request.ActorUserID && restore.ItemID == plan.Request.ImportItemID &&
		restore.SnapshotID == plan.Source.SourceSnapshotID && restore.ProviderID == plan.Source.ProviderID &&
		restore.TargetID == plan.Source.TargetID && (restore.State == "ACTIVE" || restore.State == "FINISHED") &&
		restore.HardExpiresAtMS > plan.NowMS
}

func previewRestoreContent(restore PreviewRestore, plan PreviewCreatePlan) bool {
	return restore.ContentBlobID == plan.Content.BlobID && restore.ContentName == plan.Content.LogicalName &&
		restore.ContentFormat == plan.Content.Format && restore.DependencySnapshot == plan.Source.DependencySnapshot &&
		restore.BlobID != ""
}

type previewFileIdentity struct {
	role, name, path, blob string
	order                  int
}

func previewFileKey(file PreviewFile) previewFileIdentity {
	key := previewFileIdentity{role: file.Role, name: file.LogicalName, blob: file.BlobID, order: file.SortOrder}
	if file.VirtualPath != nil {
		key.path = *file.VirtualPath
	}
	return key
}

func samePreviewFiles(left, right []PreviewFile) bool {
	if len(left) != len(right) {
		return false
	}
	expected := make(map[previewFileIdentity]int, len(left))
	for _, file := range left {
		expected[previewFileKey(file)]++
	}
	for _, file := range right {
		key := previewFileKey(file)
		if expected[key] == 0 {
			return false
		}
		expected[key]--
	}
	return true
}
