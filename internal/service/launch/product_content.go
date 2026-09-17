package launch

import (
	"cmp"
	"errors"
	model "retrom/internal/model/launch"
	"slices"

	"retrom/internal/capability/content/corevalidation"
)

func BuildProductContent(snapshot model.ProductSnapshot) (model.ProductContent, error) {
	source := snapshot.Source
	switch source.DeliveryProfile {
	case "EMULATORJS_CONTENT":
		if source.ContentKind == corevalidation.MultiDiscContentKind {
			return productMultiDiscContent(snapshot)
		}
		if source.ContentKind == "DOS_BUNDLE" {
			file, found := productFile(snapshot.VariantFiles, "DOS_LAUNCH_BUNDLE", "game.zip")
			if !found {
				return model.ProductContent{}, model.ErrBlocked
			}
			return model.ProductContent{
				Files: []model.ProductContentFile{{BlobID: file.BlobID, LogicalName: "game.zip", Format: "RETROM_DOS_DIRECT_ZIP_V1"}},
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
		return model.ProductContent{}, model.ErrBlocked
	}
}

func productFile(files []model.ProductFile, role, name string) (model.ProductFile, bool) {
	for _, file := range files {
		if file.Role == role && (name == "" || file.LogicalName == name) {
			return file, true
		}
	}
	return model.ProductFile{}, false
}

func productSingleContent(snapshot model.ProductSnapshot) (model.ProductContent, error) {
	file, found := productFile(snapshot.GameFiles, "CONTENT", "")
	if !found {
		return model.ProductContent{}, model.ErrBlocked
	}
	return model.ProductContent{
		Files: []model.ProductContentFile{{BlobID: file.BlobID, LogicalName: file.LogicalName, Format: "SOURCE_V1"}},
	}, nil
}

func productProjectContent(snapshot model.ProductSnapshot) (model.ProductContent, error) {
	files := make([]model.ProductContentFile, 0)
	for _, file := range snapshot.GameFiles {
		if file.Role != "PROJECT_FILE" {
			continue
		}
		if len(files) >= 100_000 {
			return model.ProductContent{}, model.ErrBlocked
		}
		files = append(
			files,
			model.ProductContentFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Format: snapshot.Source.ContentKind},
		)
	}
	if len(files) == 0 {
		return model.ProductContent{}, model.ErrBlocked
	}
	return model.ProductContent{Files: files}, nil
}

func productRPGContent(snapshot model.ProductSnapshot) (model.ProductContent, error) {
	if snapshot.Source.ContentKind != "RPG_MAKER_PROJECT" {
		return model.ProductContent{}, model.ErrBlocked
	}
	inputs := make([]model.PreviewFile, 0)
	for _, file := range snapshot.GameFiles {
		if file.Role == "PROJECT_FILE" {
			inputs = append(inputs, model.PreviewFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Role: file.Role})
		}
	}
	for _, file := range snapshot.VariantFiles {
		if file.Role == "RPG_EASYRPG_INDEX" || file.Role == "RPG_MAKER_LAUNCH_BUNDLE" {
			inputs = append(inputs, model.PreviewFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Role: file.Role})
		}
	}
	slices.SortFunc(inputs, func(left, right model.PreviewFile) int { return cmp.Compare(left.LogicalName, right.LogicalName) })
	role, native, err := RPGContentPolicy(snapshot.Source.DeliveryProfile)
	if err != nil {
		return model.ProductContent{}, errors.Join(model.ErrBlocked, err)
	}
	prepared, err := RPGContentFiles(inputs, role, native)
	if err != nil {
		return model.ProductContent{}, errors.Join(model.ErrBlocked, err)
	}
	files := make([]model.ProductContentFile, 0, len(prepared))
	for _, file := range prepared {
		files = append(
			files,
			model.ProductContentFile{BlobID: file.BlobID, LogicalName: file.LogicalName, Format: snapshot.Source.ContentKind},
		)
	}
	return model.ProductContent{Files: files}, nil
}
