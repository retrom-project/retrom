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

type runtimeProjectContentDefinition struct {
	contentKind       string
	runtimeFamily     string
	adapterKind       string
	adapterID         string
	maximumFiles      int
	markerPath        func(string) (string, error)
	runtimeFilesValid func(string, string) bool
}

func (service *Service) buildRuntimeProjectProductContentPlan(
	ctx context.Context,
	selection launchSelection,
	definition runtimeProjectContentDefinition,
) (launchContentPlan, error) {
	if selection.contentKind != definition.contentKind {
		return launchContentPlan{}, ErrBlocked
	}
	var dependencyJSON, compatibilityJSON, adapterKind, adapterID string
	if err := service.database.QueryRowContext(ctx, `
SELECT revision.dependency_snapshot_json,artifact.compatibility_json,
 artifact.runtime_adapter_kind,artifact.adapter_id
FROM game_variant_revisions revision
JOIN core_artifacts bound_artifact ON bound_artifact.id=revision.core_artifact_id
JOIN core_artifacts artifact ON artifact.id=?
WHERE revision.id=? AND revision.game_content_revision_id=?
 AND artifact.runtime_family=? AND artifact.available_for_launch=1
 AND artifact.core_id=bound_artifact.core_id AND artifact.route_key=bound_artifact.route_key
 AND json_extract(artifact.compatibility_json,'$.gameCompatibilityLine')=
     json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
`, selection.artifactID, selection.variantRevisionID, selection.contentRevisionID,
		definition.runtimeFamily).Scan(
		&dependencyJSON, &compatibilityJSON, &adapterKind, &adapterID,
	); err != nil || adapterKind != definition.adapterKind || adapterID != definition.adapterID {
		return launchContentPlan{}, ErrBlocked
	}
	markerPath, err := definition.markerPath(dependencyJSON)
	if err != nil || !definition.runtimeFilesValid(compatibilityJSON, selection.runtimeVersion) {
		return launchContentPlan{}, ErrBlocked
	}
	files, err := service.projectProductFiles(
		ctx, selection.contentRevisionID, definition.contentKind, definition.maximumFiles,
	)
	if err != nil || !validRuntimeLockedProjectFiles(files, markerPath, definition) {
		return launchContentPlan{}, ErrBlocked
	}
	return launchContentPlan{ContentKind: definition.contentKind, Files: files}, nil
}

func validRuntimeLockedProjectFiles(
	files []lockedContentFile,
	markerPath string,
	definition runtimeProjectContentDefinition,
) bool {
	if len(files) < 1 || len(files) > definition.maximumFiles {
		return false
	}
	seen := make(map[string]struct{}, len(files))
	markerFound := false
	for _, file := range files {
		normalized, err := importing.ValidateLogicalPath(file.LogicalName)
		if err != nil || normalized != file.LogicalName || file.BlobID == "" ||
			file.Format != definition.contentKind {
			return false
		}
		folded := importing.ASCIICaseFold(normalized)
		if _, duplicate := seen[folded]; duplicate {
			return false
		}
		seen[folded] = struct{}{}
		markerFound = markerFound || normalized == markerPath
	}
	return markerFound
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
