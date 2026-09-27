package launch

import (
	"fmt"

	butter "retrom/internal/core/butterscotch/detector"
	daphne "retrom/internal/core/daphne/detector"
	kiri "retrom/internal/core/kirikiri/detector"
	nx "retrom/internal/core/nxengine/detector"
	ons "retrom/internal/core/ons/detector"
	"retrom/internal/core/scummvm"
	tyrano "retrom/internal/core/tyranoscript/detector"
)

func previewProjectContent(snapshot PreviewSnapshot) (PreviewContent, error) {
	if snapshot.Source.ContentKind == rpgProjectFormat {
		return previewRPGContent(snapshot)
	}
	marker, err := previewProjectMarker(snapshot)
	if err != nil {
		return PreviewContent{}, ErrReviewPreviewUnavailable
	}
	content := PreviewContent{Format: snapshot.Source.ContentKind, Files: make([]PreviewFile, 0)}
	maximum := 10_000
	if snapshot.Source.ContentKind == "ONS_PROJECT" {
		maximum = MaximumProjectFiles
	}
	for _, file := range snapshot.SourceFiles {
		if file.Role != "PROJECT_FILE" {
			continue
		}
		if file.LogicalName == marker {
			content.FileRecord, content.LogicalName = file.FileRecord, file.LogicalName
			continue
		}
		if len(content.Files) >= maximum {
			return PreviewContent{}, ErrReviewPreviewUnavailable
		}
		content.Files = append(content.Files, file)
	}
	if content.FileRecord == "" || (snapshot.Source.ContentKind == "ONS_PROJECT" && len(content.Files) == 0) {
		return PreviewContent{}, ErrReviewPreviewUnavailable
	}
	return content, nil
}

func previewProjectMarker(snapshot PreviewSnapshot) (string, error) {
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
	case "DAPHNE_PROJECT":
		profile, err := daphne.ParseSnapshot(raw)
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
	return "", ErrReviewPreviewUnavailable
}

func previewMarkerResult(marker string, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("read preview project marker: %w", err)
	}
	return marker, nil
}
