package launch

import (
	"fmt"

	butter "retrom/internal/capability/engine/butterscotch/detector"
	kiri "retrom/internal/capability/engine/kirikiri/detector"
	nx "retrom/internal/capability/engine/nxengine/detector"
	ons "retrom/internal/capability/engine/ons/detector"
	"retrom/internal/capability/engine/scummvm"
	tyrano "retrom/internal/capability/engine/tyranoscript/detector"
	model "retrom/internal/model/launch"
)

func previewProjectContent(snapshot model.PreviewSnapshot) (model.PreviewContent, error) {
	if snapshot.Source.ContentKind == rpgProjectFormat {
		return previewRPGContent(snapshot)
	}
	marker, err := previewProjectMarker(snapshot)
	if err != nil {
		return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
	}
	content := model.PreviewContent{Format: snapshot.Source.ContentKind, Files: make([]model.PreviewFile, 0)}
	maximum := 10_000
	if snapshot.Source.ContentKind == "ONS_PROJECT" {
		maximum = MaximumProjectFiles
	}
	for _, file := range snapshot.SourceFiles {
		if file.Role != "PROJECT_FILE" {
			continue
		}
		if file.LogicalName == marker {
			content.BlobID, content.LogicalName = file.BlobID, file.LogicalName
			continue
		}
		if len(content.Files) >= maximum {
			return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
		}
		content.Files = append(content.Files, file)
	}
	if content.BlobID == "" || (snapshot.Source.ContentKind == "ONS_PROJECT" && len(content.Files) == 0) {
		return model.PreviewContent{}, model.ErrReviewPreviewUnavailable
	}
	return content, nil
}

func previewProjectMarker(snapshot model.PreviewSnapshot) (string, error) {
	raw := snapshot.Source.DependencySnapshot
	switch snapshot.Source.ContentKind {
	case "ONS_PROJECT":
		profile, err := ons.ParseSnapshot(raw)
		return previewMarkerResult(profile.MarkerPath, err)
	case "KIRIKIRI_PROJECT":
		profile, err := kiri.ParseSnapshot(raw)
		return previewMarkerResult(profile.MarkerPath, err)
	case "NXENGINE_PROJECT":
		profile, err := nx.ParseSnapshot(raw)
		return previewMarkerResult(profile.MarkerPath, err)
	case "BUTTERSCOTCH_PROJECT":
		profile, err := butter.ParseSnapshot(raw)
		return previewMarkerResult(profile.MarkerPath, err)
	case "TYRANOSCRIPT_PROJECT":
		profile, err := tyrano.ParseSnapshot(raw)
		return previewMarkerResult(profile.EntryPath, err)
	case "SCUMMVM_PROJECT":
		profile, err := scummvm.ParseSnapshot(raw)
		if err != nil {
			return previewMarkerResult("", err)
		}
		if _, err := profile.Selected(); err != nil {
			return previewMarkerResult("", err)
		}
		first := ""
		for _, file := range snapshot.SourceFiles {
			if file.Role == "PROJECT_FILE" && (first == "" || file.LogicalName < first) {
				first = file.LogicalName
			}
		}
		if first != "" {
			return first, nil
		}
	}
	return "", model.ErrReviewPreviewUnavailable
}

func previewMarkerResult(marker string, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("read preview project marker: %w", err)
	}
	return marker, nil
}
