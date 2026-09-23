package launch

import (
	"fmt"
	"path"
	"strings"

	"retrom/internal/corevalidation"
)

func productExternalFiles(snapshot ProductSnapshot, content ProductContent) ([]ProductExternalFile, error) {
	files := make([]ProductExternalFile, 0)
	for _, disc := range content.Discs {
		files = append(
			files,
			ProductExternalFile{Kind: "DISC", BlobID: disc.BlobID, LogicalName: disc.LogicalName, VirtualPath: disc.VirtualPath},
		)
	}
	if snapshot.Source.DeliveryProfile == "EMULATORJS_CONTENT" ||
		snapshot.Source.ProviderID == "retrom-runtime" && snapshot.Source.TargetID == "bbc-jsbeeb" {
		dependencies, err := corevalidation.ParseRuntimeBIOSDependencies(snapshot.Source.DependencySnapshot)
		if err != nil {
			return nil, ErrBlocked
		}
		files, err = productExternalBIOS(content.Files[0].LogicalName, files, dependencies, false)
		if err != nil {
			return nil, err
		}
	}
	return append(files, ProductBundleFiles(snapshot.VariantFiles)...), nil
}

func ProductBundleFiles(inputs []ProductFile) []ProductExternalFile {
	files := make([]ProductExternalFile, 0)
	for _, file := range inputs {
		if file.Role != "BIOS_BUNDLE" && file.Role != "PARENT" {
			continue
		}
		virtual := fmt.Sprintf("/__retrom__/%s/%02d/%s", strings.ToLower(file.Role), file.SortOrder, file.LogicalName)
		files = append(
			files,
			ProductExternalFile{Kind: file.Role, BlobID: file.BlobID, LogicalName: file.LogicalName, VirtualPath: virtual},
		)
	}
	return files
}

func productExternalBIOS(
	contentName string,
	files []ProductExternalFile,
	dependencies []corevalidation.BIOSDependency,
	allowMissing bool,
) ([]ProductExternalFile, error) {
	names := map[string]struct{}{strings.ToLower(path.Base(contentName)): {}}
	paths := make(map[string]struct{})
	for _, file := range files {
		key := strings.ToLower(file.LogicalName)
		if _, duplicate := names[key]; duplicate {
			return nil, ErrBlocked
		}
		if _, duplicate := paths[file.VirtualPath]; duplicate {
			return nil, ErrBlocked
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
			return nil, ErrBlocked
		}
		if len(files) >= 16 {
			return nil, ErrBlocked
		}
		key := strings.ToLower(dependency.LogicalName)
		if _, duplicate := names[key]; duplicate {
			return nil, ErrBlocked
		}
		if _, duplicate := paths[*dependency.EmulatorPath]; duplicate {
			return nil, ErrBlocked
		}
		names[key] = struct{}{}
		paths[*dependency.EmulatorPath] = struct{}{}
		files = append(
			files,
			ProductExternalFile{
				Kind:        "BIOS",
				BlobID:      *dependency.BlobID,
				LogicalName: dependency.LogicalName,
				VirtualPath: *dependency.EmulatorPath,
			},
		)
	}
	return files, nil
}

type ProductExternalSnapshot struct {
	DependencySnapshot, ContentName string
	Files                           []ProductExternalFile
}

func FreezeProductExternalBIOS(snapshot ProductExternalSnapshot, allowMissing bool) ([]ProductExternalFile, error) {
	dependencies, err := corevalidation.ParseRuntimeBIOSDependencies(snapshot.DependencySnapshot)
	if err != nil {
		return nil, ErrBlocked
	}
	files, err := productExternalBIOS(snapshot.ContentName, snapshot.Files, dependencies, allowMissing)
	if err != nil {
		return nil, err
	}
	return files[len(snapshot.Files):], nil
}
