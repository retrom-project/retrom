package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
)

type runtimeProjectIndex struct {
	Files         []runtimeProjectIndexFile `json:"files"`
	SchemaVersion int                       `json:"schemaVersion"`
}

type runtimeProjectIndexFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	URL       string `json:"url"`
}

func (service *Service) reviewPreviewProjectContent(
	ctx context.Context,
	source reviewPreviewSource,
	markerPath, format string,
	maximumFiles int,
	diagnosticName string,
) (reviewPreviewContentSet, error) {
	content := reviewPreviewContentSet{Format: format}
	if err := service.database.QueryRowContext(ctx, `
SELECT blob_id,logical_name FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role='PROJECT_FILE' AND logical_name=?
`, source.SourceSnapshotID, markerPath).Scan(&content.BlobID, &content.LogicalName); err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT logical_name,blob_id,sort_order FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role='PROJECT_FILE' AND logical_name<>?
ORDER BY sort_order,logical_name
`, source.SourceSnapshotID, markerPath)
	if err != nil {
		return reviewPreviewContentSet{}, fmt.Errorf("review %s project files: %w", diagnosticName, err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	content.Files = make([]reviewPreviewFile, 0)
	for rows.Next() {
		file := reviewPreviewFile{Role: "PROJECT_FILE"}
		if err := rows.Scan(&file.LogicalName, &file.BlobID, &file.SortOrder); err != nil ||
			len(content.Files) >= maximumFiles {
			return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
		}
		content.Files = append(content.Files, file)
	}
	if err := rows.Err(); err != nil {
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return content, nil
}

func buildRuntimeProjectIndex(
	projectRoot, markerPath string,
	files []runtimeProjectIndexFile,
	maximumFiles int,
	allowEmptyFiles bool,
	diagnosticName string,
) (ProjectIndexView, error) {
	if len(files) < 1 || len(files) > maximumFiles {
		return ProjectIndexView{}, ErrCredential
	}
	seen := make(map[string]struct{}, len(files))
	markerFound := false
	for index := range files {
		normalized, err := importing.ValidateLogicalPath(files[index].Path)
		if err != nil || normalized != files[index].Path || files[index].SizeBytes < 0 ||
			(!allowEmptyFiles && files[index].SizeBytes == 0) {
			return ProjectIndexView{}, ErrCredential
		}
		folded := importing.ASCIICaseFold(normalized)
		if _, duplicate := seen[folded]; duplicate {
			return ProjectIndexView{}, ErrCredential
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == markerPath
		files[index].URL = projectRoot + escapeProjectPath(normalized)
	}
	if !markerFound {
		return ProjectIndexView{}, ErrCredential
	}
	contents, err := json.Marshal(runtimeProjectIndex{Files: files, SchemaVersion: 1})
	if err != nil {
		return ProjectIndexView{}, fmt.Errorf("marshal %s project index: %w", diagnosticName, err)
	}
	digest := sha256.Sum256(contents)
	return ProjectIndexView{Contents: contents, SHA256: hex.EncodeToString(digest[:])}, nil
}
