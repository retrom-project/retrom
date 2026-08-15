package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/contentcapability"
)

type ServerSourceFile struct {
	RelativePath string
	BlobID       string
	SizeBytes    int64
}

const ServerSourceFileLimit = 64

type ServerImportItem struct {
	ItemID                    string
	State                     string
	ValidationStatus          string
	CompatibilityCode         string
	CoreID                    string
	CoreName                  string
	DependencySnapshotJSON    string
	ContentKind               string
	SourceManifestJSON        string
	SourceManifestDigest      string
	ExistingGameID            string
	ExistingContentRevisionID string
	ExistingMatches           []ServerDuplicateMatch
	SourceRelativePaths       []string
}

type ServerDuplicateMatch struct {
	GameID            string `json:"gameId"`
	ContentRevisionID string `json:"contentRevisionId"`
}

type ServerImportResult struct {
	Created       Created
	Items         []ServerImportItem
	RejectedCodes []string
}

type ServerMetadata struct {
	Title, Description, Developer, Publisher, Genre string
	Players, ReleaseYear                            *int
}

type serverReviewOrigin struct {
	SourceRefID string
	Assets      []ExternalAsset
}

// CreateServerSource adopts already verified CAS blobs into the established
// import/content-profile pipeline. It creates an internal COMPLETE upload
// envelope so all archive, DAT, BIOS, multi-disc, and duplicate invariants stay
// identical to browser imports.
func (service *Service) CreateServerSource(
	ctx context.Context,
	targetPlatformInstanceID, contentMode string,
	files []ServerSourceFile,
	tagIDs []string,
	assignedByUserID string,
) (ServerImportResult, error) {
	if len(files) == 0 || len(files) > ServerSourceFileLimit {
		return ServerImportResult{}, ErrInvalid
	}
	if contentMode == "" {
		contentMode = contentcapability.ModeStandard
	}
	sorted, reusable, totalBytes, err := service.validateServerFiles(ctx, files)
	if err != nil {
		return ServerImportResult{}, err
	}
	uploadID, _ := uuid.NewV7()
	manifest, _ := json.Marshal(map[string]any{"schemaVersion": 1, "files": sorted})
	digest := sha256.Sum256(manifest)
	sourceType := "FILES"
	if contentMode == contentcapability.ModeMultiDiscM3UV1 {
		sourceType = "DIRECTORY"
	}
	now := service.now().UnixMilli()
	if err := service.insertServerUpload(
		ctx,
		uploadID.String(),
		sourceType,
		reusable,
		hex.EncodeToString(digest[:]),
		now,
		totalBytes,
	); err != nil {
		return ServerImportResult{}, err
	}
	if len(tagIDs) > 0 {
		ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: assignedByUserID})
	}
	created, err := service.create(ctx, CreateRequest{
		UploadID: uploadID.String(), TargetPlatformInstanceID: targetPlatformInstanceID,
		MetadataProvider: "NONE", ContentMode: contentMode, TagIDs: tagIDs,
	}, nil)
	if err != nil {
		service.removeUnusedClonedUpload(ctx, uploadID.String())
		return ServerImportResult{}, err
	}
	return service.serverImportResult(ctx, created)
}

func (service *Service) validateServerFiles(
	ctx context.Context,
	files []ServerSourceFile,
) ([]ServerSourceFile, []reusableUploadFile, int64, error) {
	sorted := append([]ServerSourceFile(nil), files...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return sorted[left].RelativePath < sorted[right].RelativePath
	})
	reusable := make([]reusableUploadFile, 0, len(sorted))
	seen := make(map[string]struct{}, len(sorted))
	var totalBytes int64
	for index, file := range sorted {
		folded := strings.ToLower(file.RelativePath)
		if file.RelativePath == "" || file.BlobID == "" || file.SizeBytes < 0 {
			return nil, nil, 0, ErrInvalid
		}
		if _, exists := seen[folded]; exists {
			return nil, nil, 0, ErrInvalid
		}
		var size int64
		err := service.database.QueryRowContext(
			ctx,
			`SELECT size_bytes FROM blobs WHERE id=?`,
			file.BlobID,
		).Scan(&size)
		if err != nil || size != file.SizeBytes {
			return nil, nil, 0, ErrInvalid
		}
		seen[folded] = struct{}{}
		totalBytes += file.SizeBytes
		reusable = append(reusable, reusableUploadFile{
			id: fmt.Sprintf("server-%d", index), path: file.RelativePath, blobID: file.BlobID, size: file.SizeBytes,
		})
	}
	return sorted, reusable, totalBytes, nil
}

func (service *Service) insertServerUpload(
	ctx context.Context,
	uploadID, sourceType string,
	files []reusableUploadFile,
	digest string,
	now, totalBytes int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("libraryimport/server source: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if err := insertClonedUpload(ctx, transaction, uploadID, sourceType, files, digest, now, totalBytes); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("libraryimport/server source: %w", err)
	}
	return nil
}

func (service *Service) serverImportResult(ctx context.Context, created Created) (ServerImportResult, error) {
	result := ServerImportResult{Created: created}
	rows, err := service.database.QueryContext(ctx, `
SELECT item.id,item.state,COALESCE(validation.status,''),COALESCE(validation.compatibility_code,''),
COALESCE(validation.core_id,''),COALESCE(core.name,''),COALESCE(validation.dependency_snapshot_json,''),
snapshot.content_kind,snapshot.source_manifest_json,snapshot.source_manifest_digest,
COALESCE(duplicate.existing_game_id,''),COALESCE(duplicate.existing_game_content_revision_id,''),
COALESCE((SELECT json_group_array(relative_path) FROM (
 SELECT DISTINCT upload.relative_path AS relative_path
 FROM import_item_source_files source JOIN upload_files upload ON upload.id=source.upload_file_id
 WHERE source.import_item_id=item.id AND source.role IN ('CONTENT','DOS_SOURCE','PLAYLIST_SOURCE','DISC')
 ORDER BY upload.relative_path
)),'[]')
FROM import_items item
JOIN import_item_source_snapshots snapshot ON snapshot.import_item_id=item.id AND snapshot.revision_no=1
LEFT JOIN review_drafts draft ON draft.import_item_id=item.id
LEFT JOIN import_item_core_validations validation ON validation.id=COALESCE(
 draft.selected_validation_id,
 (SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=item.id
  AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
  AND candidate.target_platform_instance_id=draft.target_platform_instance_id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1)
)
LEFT JOIN cores core ON core.id=validation.core_id
LEFT JOIN import_item_duplicate_matches duplicate ON duplicate.import_item_id=item.id
WHERE item.import_job_id=?
ORDER BY item.id,duplicate.existing_game_id
`, created.ImportJobID)
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("libraryimport/server source: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	itemIndexes := map[string]int{}
	for rows.Next() {
		var item ServerImportItem
		var sourcePaths string
		if err := rows.Scan(&item.ItemID, &item.State, &item.ValidationStatus, &item.CompatibilityCode,
			&item.CoreID, &item.CoreName, &item.DependencySnapshotJSON,
			&item.ContentKind, &item.SourceManifestJSON, &item.SourceManifestDigest,
			&item.ExistingGameID, &item.ExistingContentRevisionID, &sourcePaths); err != nil {
			return ServerImportResult{}, fmt.Errorf("libraryimport/server source: %w", err)
		}
		_ = json.Unmarshal([]byte(sourcePaths), &item.SourceRelativePaths)
		if index, exists := itemIndexes[item.ItemID]; exists {
			if item.ExistingGameID != "" {
				result.Items[index].ExistingMatches = append(
					result.Items[index].ExistingMatches,
					ServerDuplicateMatch{
						GameID:            item.ExistingGameID,
						ContentRevisionID: item.ExistingContentRevisionID,
					},
				)
			}
			continue
		}
		if item.ExistingGameID != "" {
			item.ExistingMatches = append(
				item.ExistingMatches,
				ServerDuplicateMatch{GameID: item.ExistingGameID, ContentRevisionID: item.ExistingContentRevisionID},
			)
		}
		itemIndexes[item.ItemID] = len(result.Items)
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ServerImportResult{}, fmt.Errorf("libraryimport/server source: %w", err)
	}
	result.RejectedCodes, err = service.serverImportRejectedCodes(ctx, created.ImportJobID)
	if err != nil {
		return ServerImportResult{}, err
	}
	return result, nil
}

func (service *Service) serverImportRejectedCodes(ctx context.Context, importJobID string) ([]string, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT DISTINCT COALESCE(reason_code,'IMPORT_INVALID') FROM import_job_files
WHERE import_job_id=? AND disposition='REJECTED' ORDER BY 1
`, importJobID)
	if err != nil {
		return nil, fmt.Errorf("libraryimport/server source: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	codes := make([]string, 0)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("libraryimport/server source: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("libraryimport/server source: %w", err)
	}
	return codes, nil
}

func (service *Service) patchServerMetadata(
	ctx context.Context,
	itemID string,
	metadata ServerMetadata,
) (int64, error) {
	if !validServerMetadata(metadata) {
		return 0, ErrInvalid
	}
	encoded, err := json.Marshal(map[string]any{
		"title": metadata.Title, "description": metadata.Description, "developer": metadata.Developer,
		"publisher": metadata.Publisher, "genre": metadata.Genre, "players": metadata.Players,
		"releaseYear": metadata.ReleaseYear,
	})
	if err != nil {
		return 0, fmt.Errorf("libraryimport/server metadata: %w", err)
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("libraryimport/server metadata: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var before string
	var version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT draft.metadata_json,draft.version FROM review_drafts draft
JOIN import_items item ON item.id=draft.import_item_id
WHERE draft.import_item_id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(&before, &version); err != nil {
		return 0, ErrInvalid
	}
	if before == string(encoded) {
		return version, nil
	}
	now := service.now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_drafts SET metadata_json=?,version=version+1,updated_at_ms=?
WHERE import_item_id=? AND version=?
`, string(encoded), now, itemID, version); err != nil {
		return 0, fmt.Errorf("libraryimport/server metadata: %w", err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE import_items SET search_text=? WHERE id=?`,
		strings.ToLower(metadata.Title),
		itemID,
	); err != nil {
		return 0, fmt.Errorf("libraryimport/server metadata: %w", err)
	}
	eventID, _ := uuid.NewV7()
	actor := reviewActor(ctx)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DRAFT_SAVED',?,?,?,?,?,?,'{}','{}','{}',?)
`, eventID.String(), itemID, actor.Kind, actor.UserID, actor.Label, before,
		string(encoded), string(encoded), now); err != nil {
		return 0, fmt.Errorf("libraryimport/server metadata: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("libraryimport/server metadata: %w", err)
	}
	return version + 1, nil
}

func validServerMetadata(metadata ServerMetadata) bool {
	if metadata.Title == "" || !validField(metadata.Title, 200, false) {
		return false
	}
	for _, value := range []struct {
		text      string
		maximum   int
		multiline bool
	}{
		{metadata.Description, 20_000, true},
		{metadata.Developer, 500, false},
		{metadata.Publisher, 500, false},
		{metadata.Genre, 500, false},
	} {
		if !validField(value.text, value.maximum, value.multiline) {
			return false
		}
	}
	if metadata.Players != nil && (*metadata.Players < 1 || *metadata.Players > 64) {
		return false
	}
	return metadata.ReleaseYear == nil || *metadata.ReleaseYear >= 1000 && *metadata.ReleaseYear <= 9999
}

// SeedServerReviewMetadata applies the trusted, frozen Pegasus text fields to
// the ordinary review draft. Publication remains an explicit review decision.
func (service *Service) SeedServerReviewMetadata(
	ctx context.Context,
	importItemID string,
	metadata ServerMetadata,
) (int64, error) {
	return service.patchServerMetadata(ctx, importItemID, metadata)
}

func validExternalAssets(assets []ExternalAsset) bool {
	seen := map[string]struct{}{}
	for _, asset := range assets {
		if _, exists := seen[asset.Kind]; exists || asset.BlobID == "" {
			return false
		}
		seen[asset.Kind] = struct{}{}
		if !validExternalAsset(asset) {
			return false
		}
	}
	return true
}

func validExternalAsset(asset ExternalAsset) bool {
	switch asset.Kind {
	case "COVER":
		validDimensions := asset.WidthPX != nil && asset.HeightPX != nil &&
			*asset.WidthPX > 0 && *asset.HeightPX > 0
		validType := asset.MediaType == "image/png" || asset.MediaType == "image/jpeg" ||
			asset.MediaType == "image/webp"
		return validDimensions && validType
	case "VIDEO":
		validType := asset.MediaType == "video/mp4" || asset.MediaType == "video/webm"
		return asset.WidthPX == nil && asset.HeightPX == nil && validType
	default:
		return false
	}
}

func loadServerReviewOrigin(
	ctx context.Context,
	transaction *sql.Tx,
	importItemID string,
	coverOverridden bool,
) (serverReviewOrigin, bool, error) {
	var origin serverReviewOrigin
	if err := transaction.QueryRowContext(ctx, `
SELECT id
FROM pegasus_import_items
WHERE library_import_item_id=? AND execution_state='REVIEW_PENDING'
`, importItemID).Scan(&origin.SourceRefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return serverReviewOrigin{}, false, nil
		}
		return serverReviewOrigin{}, false, fmt.Errorf("libraryimport/server review origin: %w", err)
	}
	rows, err := transaction.QueryContext(ctx, `
SELECT kind,blob_id,media_type,width_px,height_px
FROM pegasus_import_item_assets
WHERE item_id=? AND state='COPIED' AND blob_id IS NOT NULL AND media_type IS NOT NULL
ORDER BY CASE kind WHEN 'COVER' THEN 0 ELSE 1 END
`, origin.SourceRefID)
	if err != nil {
		return serverReviewOrigin{}, false, fmt.Errorf("libraryimport/server review assets: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var asset ExternalAsset
		var width, height sql.NullInt64
		if err := rows.Scan(&asset.Kind, &asset.BlobID, &asset.MediaType, &width, &height); err != nil {
			return serverReviewOrigin{}, false, fmt.Errorf("libraryimport/server review assets: %w", err)
		}
		if asset.Kind == "COVER" && coverOverridden {
			continue
		}
		if width.Valid {
			asset.WidthPX = &width.Int64
		}
		if height.Valid {
			asset.HeightPX = &height.Int64
		}
		if !validExternalAsset(asset) {
			return serverReviewOrigin{}, false, ErrInvalid
		}
		origin.Assets = append(origin.Assets, asset)
	}
	if err := rows.Err(); err != nil {
		return serverReviewOrigin{}, false, fmt.Errorf("libraryimport/server review assets: %w", err)
	}
	return origin, true, nil
}

func transitionServerReview(
	ctx context.Context,
	transaction *sql.Tx,
	importItemID, state string,
	gameID any,
	now int64,
) error {
	if state != "PUBLISHED" && state != "REVIEW_DISCARDED" {
		return ErrInvalid
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items
SET execution_state=?,published_game_id=?,updated_at_ms=?
WHERE library_import_item_id=? AND execution_state='REVIEW_PENDING'
`, state, gameID, now, importItemID)
	if err != nil {
		return fmt.Errorf("libraryimport/server review transition: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("libraryimport/server review rows affected: %w", err)
	}
	if affected == 0 {
		var linked int
		if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM pegasus_import_items WHERE library_import_item_id=?
`, importItemID).Scan(&linked); err != nil {
			return fmt.Errorf("libraryimport/server review link: %w", err)
		}
		if linked > 0 {
			return ErrInvalid
		}
		return nil
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports
SET review_pending_item_count=(
  SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='REVIEW_PENDING'
),
published_item_count=(
  SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='PUBLISHED'
),
review_discarded_item_count=(
  SELECT count(*) FROM pegasus_import_items item
  WHERE item.import_id=pegasus_imports.id AND item.execution_state='REVIEW_DISCARDED'
),
version=version+1,updated_at_ms=?
WHERE id=(SELECT import_id FROM pegasus_import_items WHERE library_import_item_id=? LIMIT 1)
`, now, importItemID); err != nil {
		return fmt.Errorf("libraryimport/server review aggregate: %w", err)
	}
	return nil
}

func (service *Service) copyExternalAssets(
	ctx context.Context,
	transaction *sql.Tx,
	gameID, metadataID string,
	assets []ExternalAsset,
	now int64,
) error {
	for _, asset := range assets {
		var mediaType string
		err := transaction.QueryRowContext(
			ctx,
			`SELECT media_type FROM blobs WHERE id=?`,
			asset.BlobID,
		).Scan(&mediaType)
		if err != nil ||
			mediaType != asset.MediaType {
			return ErrInvalid
		}
		assetID, _ := uuid.NewV7()
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO game_assets(
id,game_id,metadata_revision_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms
)
VALUES(?,?,?,?,?,0,?,?,?,?)
`, assetID.String(), gameID, metadataID, asset.BlobID, asset.Kind,
			asset.WidthPX, asset.HeightPX, asset.MediaType, now); err != nil {
			return fmt.Errorf("libraryimport/server asset: %w", err)
		}
	}
	return nil
}
