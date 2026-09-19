package launch

import (
	"cmp"
	"slices"
	"strings"

	"retrom/internal/capability/content/contentprofile"
	model "retrom/internal/model/launch"
)

func previewContent(snapshot model.PreviewSnapshot) (model.PreviewContent, error) {
	source := snapshot.Source
	if contentprofile.IsProjectContentKind(contentprofile.ContentKind(source.ContentKind)) {
		content, err := previewProjectContent(snapshot)
		if err != nil {
			return model.PreviewContent{}, err
		}
		if !validPreviewFileSet(content.LogicalName, content.Files) {
			return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
		}
		return content, nil
	}
	content, err := previewPrimaryContent(snapshot)
	if err != nil {
		return model.PreviewContent{}, err
	}
	for _, file := range snapshot.ValidationFiles {
		if file.Role != "PARENT" && file.Role != "BIOS_BUNDLE" {
			continue
		}
		if len(content.Files) >= 16 || !validPreviewLogicalName(file.LogicalName) {
			return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
		}
		content.Files = append(content.Files, file)
	}
	if source.DATVersionID == nil {
		content.Files, err = reviewPreviewExternalFiles(source.DependencySnapshot, content.Files)
		if err != nil {
			return model.PreviewContent{}, err
		}
	}
	if !validPreviewFileSet(content.LogicalName, content.Files) {
		return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
	}
	return content, nil
}

func previewPrimaryContent(snapshot model.PreviewSnapshot) (model.PreviewContent, error) {
	switch snapshot.Source.ContentKind {
	case "SINGLE_FILE":
		for _, file := range snapshot.SourceFiles {
			if file.Role == "CONTENT" {
				return model.PreviewContent{BlobID: file.BlobID, LogicalName: file.LogicalName, Format: "SOURCE_V1"}, nil
			}
		}
	case "DOS_BUNDLE":
		return validatedPreviewContent(snapshot, "DOS_LAUNCH_BUNDLE", "game.zip", "RETROM_DOS_DIRECT_ZIP_V1")
	case "MULTI_DISC":
		if snapshot.Source.ValidationStatus != "READY" {
			return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
		}
		content, err := validatedPreviewContent(snapshot, "MULTI_DISC_PLAYLIST", "playlist.m3u", "RETROM_MULTIDISC_M3U_V1")
		if err != nil {
			return model.PreviewContent{}, err
		}
		for _, file := range snapshot.SourceFiles {
			if file.Role != "DISC" {
				continue
			}
			virtualPath := "/" + file.LogicalName
			file.VirtualPath = &virtualPath
			content.Files = append(content.Files, file)
		}
		if len(content.Files) < 2 || len(content.Files) > 8 {
			return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
		}
		return content, nil
	}
	return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
}

func validatedPreviewContent(snapshot model.PreviewSnapshot, role, name, format string) (model.PreviewContent, error) {
	for _, file := range snapshot.ValidationFiles {
		if file.Role == role && file.LogicalName == name {
			return model.PreviewContent{BlobID: file.BlobID, LogicalName: name, Format: format}, nil
		}
	}
	return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
}

func previewRPGContent(snapshot model.PreviewSnapshot) (model.PreviewContent, error) {
	files := make([]model.PreviewFile, 0)
	for _, file := range snapshot.SourceFiles {
		if file.Role == "PROJECT_FILE" {
			files = append(files, file)
		}
	}
	for _, file := range snapshot.ValidationFiles {
		if file.Role == "RPG_EASYRPG_INDEX" || file.Role == "RPG_MAKER_LAUNCH_BUNDLE" {
			files = append(files, file)
		}
	}
	slices.SortStableFunc(
		files,
		func(left, right model.PreviewFile) int { return cmp.Compare(left.LogicalName, right.LogicalName) },
	)
	role, native, err := RPGContentPolicy(snapshot.Source.DeliveryProfile)
	if err != nil {
		return model.PreviewContent{}, err
	}
	files, err = RPGContentFiles(files, role, native)
	if err != nil {
		return model.PreviewContent{}, err
	}
	content := model.PreviewContent{Format: rpgProjectFormat, Files: make([]model.PreviewFile, 0, len(files))}
	for _, file := range files {
		file.Role = "PROJECT_FILE"
		if strings.HasPrefix(file.LogicalName, "__retrom__/") {
			file.Role = "RUNTIME_FILE"
		} else if content.BlobID == "" {
			content.BlobID, content.LogicalName = file.BlobID, file.LogicalName
			continue
		}
		file.SortOrder = len(content.Files)
		content.Files = append(content.Files, file)
	}
	return content, nil
}
