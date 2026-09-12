package launch

import (
	"cmp"
	"errors"
	"slices"

	"retrom/internal/corevalidation"
)

func BuildProductContent(snapshot ProductSnapshot) (ProductContent, error) {
	source := snapshot.Source
	switch source.DeliveryProfile {
	case "EMULATORJS_CONTENT":
		if source.ContentKind == corevalidation.MultiDiscContentKind {
			return productMultiDiscContent(snapshot)
		}
		if source.ContentKind == "DOS_BUNDLE" {
			file, found := productFile(snapshot.VariantFiles, "DOS_LAUNCH_BUNDLE", "game.zip")
			if !found {
				return ProductContent{}, ErrBlocked
			}
			return ProductContent{
				Files: []ProductContentFile{{BlobID: file.BlobID, LogicalName: "game.zip", Format: "RETROM_DOS_DIRECT_ZIP_V1"}},
			}, nil
		}
		return productSingleContent(snapshot)
	case "ROM_BLOB":
		return productSingleContent(snapshot)
	case "FILE_TREE_PROJECT", "ISOLATED_WEB_PROJECT":
		if source.ContentKind == "RPG_MAKER_PROJECT" {
			return productRPGContent(snapshot)
		}
		return productProjectContent(snapshot)
	case "SEEKABLE_PROJECT_ARCHIVE":
		return productRPGContent(snapshot)
	default:
		return ProductContent{}, ErrBlocked
	}
}

func productFile(files []ProductFile, role, name string) (ProductFile, bool) {
	for _, file := range files {
		if file.Role == role && (name == "" || file.LogicalName == name) {
			return file, true
		}
	}
	return ProductFile{}, false
}

func productSingleContent(snapshot ProductSnapshot) (ProductContent, error) {
	file, found := productFile(snapshot.GameFiles, "CONTENT", "")
	if !found {
		return ProductContent{}, ErrBlocked
	}
	return ProductContent{
		Files: []ProductContentFile{{BlobID: file.BlobID, LogicalName: file.LogicalName, Format: "SOURCE_V1"}},
	}, nil
}

func productProjectContent(snapshot ProductSnapshot) (ProductContent, error) {
	files := make([]ProductContentFile, 0)
	for _, file := range snapshot.GameFiles {
		if file.Role != "PROJECT_FILE" {
			continue
		}
		if len(files) >= 100_000 {
			return ProductContent{}, ErrBlocked
		}
		files = append(
			files,
			ProductContentFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Format: snapshot.Source.ContentKind},
		)
	}
	if len(files) == 0 {
		return ProductContent{}, ErrBlocked
	}
	return ProductContent{Files: files}, nil
}

func productRPGContent(snapshot ProductSnapshot) (ProductContent, error) {
	if snapshot.Source.ContentKind != "RPG_MAKER_PROJECT" {
		return ProductContent{}, ErrBlocked
	}
	inputs := make([]PreviewFile, 0)
	for _, file := range snapshot.GameFiles {
		if file.Role == "PROJECT_FILE" {
			inputs = append(inputs, PreviewFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Role: file.Role})
		}
	}
	for _, file := range snapshot.VariantFiles {
		if file.Role == "RPG_EASYRPG_INDEX" || file.Role == "RPG_MAKER_LAUNCH_BUNDLE" {
			inputs = append(inputs, PreviewFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Role: file.Role})
		}
	}
	slices.SortFunc(inputs, func(left, right PreviewFile) int { return cmp.Compare(left.LogicalName, right.LogicalName) })
	role, native, err := RPGContentPolicy(snapshot.Source.DeliveryProfile)
	if err != nil {
		return ProductContent{}, errors.Join(ErrBlocked, err)
	}
	prepared, err := RPGContentFiles(inputs, role, native)
	if err != nil {
		return ProductContent{}, errors.Join(ErrBlocked, err)
	}
	files := make([]ProductContentFile, 0, len(prepared))
	for _, file := range prepared {
		files = append(
			files,
			ProductContentFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Format: snapshot.Source.ContentKind},
		)
	}
	return ProductContent{Files: files}, nil
}
