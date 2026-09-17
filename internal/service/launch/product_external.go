package launch

import (
	"fmt"
	"path"
	model "retrom/internal/model/launch"
	"strings"

	"retrom/internal/capability/content/corevalidation"
)

func productExternalFiles(snapshot model.ProductSnapshot, content model.ProductContent) ([]model.ProductExternalFile, error) {
	files := make([]model.ProductExternalFile, 0)
	for _, disc := range content.Discs {
		files = append(
			files,
			model.ProductExternalFile{Kind: "DISC", BlobID: disc.BlobID, LogicalName: disc.LogicalName, VirtualPath: disc.VirtualPath},
		)
	}
	if snapshot.Source.DeliveryProfile == "EMULATORJS_CONTENT" {
		dependencies, err := corevalidation.ParseRuntimeBIOSDependencies(snapshot.Source.DependencySnapshot)
		if err != nil {
			return nil, model.ErrBlocked
		}
		files, err = productExternalBIOS(content.Files[0].LogicalName, files, dependencies, false)
		if err != nil {
			return nil, err
		}
	}
	return append(files, ProductBundleFiles(snapshot.VariantFiles)...), nil
}

func ProductBundleFiles(inputs []model.ProductFile) []model.ProductExternalFile {
	files := make([]model.ProductExternalFile, 0)
	for _, file := range inputs {
		if file.Role != "BIOS_BUNDLE" && file.Role != "PARENT" {
			continue
		}
		virtual := fmt.Sprintf("/__retrom__/%s/%02d/%s", strings.ToLower(file.Role), file.SortOrder, file.LogicalName)
		files = append(
			files,
			model.ProductExternalFile{Kind: file.Role, BlobID: file.BlobID, LogicalName: file.LogicalName, VirtualPath: virtual},
		)
	}
	return files
}

func productExternalBIOS(
	contentName string,
	files []model.ProductExternalFile,
	dependencies []corevalidation.BIOSDependency,
	allowMissing bool,
) ([]model.ProductExternalFile, error) {
	names := map[string]struct{}{strings.ToLower(path.Base(contentName)): {}}
	paths := make(map[string]struct{})
	for _, file := range files {
		key := strings.ToLower(file.LogicalName)
		if _, duplicate := names[key]; duplicate {
			return nil, model.ErrBlocked
		}
		if _, duplicate := paths[file.VirtualPath]; duplicate {
			return nil, model.ErrBlocked
		}
		names[key] = struct{}{}
		paths[file.VirtualPath] = struct{}{}
	}
	for _, dependency := range dependencies {
		if dependency.DeliveryKind != "EXTERNAL_FILE" {
			continue
		}
		if !availableReviewPreviewExternal(dependency) {
			if allowMissing || dependency.RequirementMode == "OPTIONAL" && dependency.InstallationStatus == nil {
				continue
			}
			return nil, model.ErrBlocked
		}
		if len(files) >= 16 {
			return nil, model.ErrBlocked
		}
		key := strings.ToLower(dependency.LogicalName)
		if _, duplicate := names[key]; duplicate {
			return nil, model.ErrBlocked
		}
		if _, duplicate := paths[*dependency.EmulatorPath]; duplicate {
			return nil, model.ErrBlocked
		}
		names[key] = struct{}{}
		paths[*dependency.EmulatorPath] = struct{}{}
		files = append(
			files,
			model.ProductExternalFile{
				Kind:        "BIOS",
				BlobID:      *dependency.BlobID,
				LogicalName: dependency.LogicalName,
				VirtualPath: *dependency.EmulatorPath,
			},
		)
	}
	return files, nil
}

func FreezeProductExternalBIOS(snapshot model.ProductExternalSnapshot, allowMissing bool) ([]model.ProductExternalFile, error) {
	dependencies, err := corevalidation.ParseRuntimeBIOSDependencies(snapshot.DependencySnapshot)
	if err != nil {
		return nil, model.ErrBlocked
	}
	files, err := productExternalBIOS(snapshot.ContentName, snapshot.Files, dependencies, allowMissing)
	if err != nil {
		return nil, err
	}
	return files[len(snapshot.Files):], nil
}
