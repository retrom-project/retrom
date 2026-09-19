package launch

import (
	"path"
	"strings"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/format/importing"
	model "retrom/internal/model/launch"
)

func reviewPreviewExternalFiles(
	dependencySnapshot string,
	files []model.PreviewFile,
) ([]model.PreviewFile, error) {
	snapshot, err := corevalidation.ParseSnapshot(dependencySnapshot)
	if err != nil {
		return nil, model.ErrReviewPreviewUnavailable
	}
	for _, dependency := range snapshot.BIOS {
		if !availableReviewPreviewExternal(dependency) {
			continue
		}
		if len(files) >= 16 || !validPreviewLogicalName(dependency.LogicalName) {
			return nil, model.ErrReviewPreviewUnavailable
		}
		files = append(files, model.PreviewFile{
			Role: "EXTERNAL_FILE", LogicalName: dependency.LogicalName, BlobID: *dependency.BlobID,
			VirtualPath: dependency.EmulatorPath, SortOrder: len(files),
		})
	}
	return files, nil
}

func availableReviewPreviewExternal(dependency corevalidation.BIOSDependency) bool {
	if dependency.DeliveryKind != "EXTERNAL_FILE" || dependency.EmulatorPath == nil || dependency.BlobID == nil ||
		dependency.InstallationStatus == nil {
		return false
	}
	return corevalidation.BIOSInstallationUsable(*dependency.InstallationStatus)
}

func validPreviewLogicalName(value string) bool {
	return value != "" && len(value) <= 255 && path.Base(value) == value && value != "." && value != ".." &&
		!strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}

func validPreviewFileSet(contentName string, files []model.PreviewFile) bool {
	seenNames := map[string]struct{}{importing.ASCIICaseFold(contentName): {}}
	seenPaths := make(map[string]struct{})
	for _, file := range files {
		if file.Role == "PROJECT_FILE" || file.Role == "RUNTIME_FILE" {
			if _, err := importing.ValidateLogicalPath(file.LogicalName); err != nil || file.VirtualPath != nil {
				return false
			}
		} else if !validPreviewLogicalName(file.LogicalName) {
			return false
		}
		name := importing.ASCIICaseFold(file.LogicalName)
		if _, duplicate := seenNames[name]; duplicate {
			return false
		}
		seenNames[name] = struct{}{}
		if file.VirtualPath != nil {
			if _, duplicate := seenPaths[*file.VirtualPath]; duplicate {
				return false
			}
			seenPaths[*file.VirtualPath] = struct{}{}
		}
	}
	return true
}
