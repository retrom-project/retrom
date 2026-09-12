package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"retrom/internal/dbexec"
	librarypersistence "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"

	"retrom/internal/persistence/recordstore"

	"github.com/google/uuid"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
)

type ServerSourceFile = libraryservice.ServerSourceFile

const ServerSourceFileLimit = 64

const (
	reviewHandoffDirect           = "DIRECT"
	reviewHandoffEmulationStation = "EMULATIONSTATION"
)

type (
	ServerImportItem     = libraryservice.ServerImportItem
	ServerDuplicateMatch = libraryservice.ServerDuplicateMatch
	ServerImportResult   = libraryservice.ServerImportResult
)

type (
	ServerMetadata        = libraryservice.ServerMetadata
	ServerMetadataWarning = libraryservice.ServerMetadataWarning
)

type serverReviewOrigin struct {
	SourceRefID string
	SourceKind  string
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
	return service.createServerSource(
		ctx, "", targetPlatformInstanceID, contentMode, files, tagIDs, assignedByUserID,
	)
}

// CreateServerSourceOnce gives a server-owned source item an idempotent handoff
// into the ordinary import pipeline. Repeating the same key and frozen inputs
// returns the original import instead of creating a second hidden review.
func (service *Service) CreateServerSourceOnce(
	ctx context.Context,
	idempotencyKey, targetPlatformInstanceID, contentMode string,
	files []ServerSourceFile,
	tagIDs []string,
	assignedByUserID string,
) (ServerImportResult, error) {
	if idempotencyKey == "" {
		return ServerImportResult{}, ErrInvalid
	}
	return service.createServerSource(
		ctx, idempotencyKey, targetPlatformInstanceID, contentMode, files, tagIDs, assignedByUserID,
	)
}

func (service *Service) createServerSource(
	ctx context.Context,
	idempotencyKey, targetPlatformInstanceID, contentMode string,
	files []ServerSourceFile,
	tagIDs []string,
	assignedByUserID string,
) (ServerImportResult, error) {
	prepared, err := service.prepareServerSource(ctx, idempotencyKey, contentMode, files)
	if err != nil {
		return ServerImportResult{}, err
	}
	target, err := service.loadCreationTarget(ctx, targetPlatformInstanceID)
	if err != nil {
		return ServerImportResult{}, err
	}
	prepared.contentMode = normalizeTargetContentMode(target.platformID, prepared.contentMode)
	created, found, err := service.ensureServerSourceUpload(ctx, prepared, targetPlatformInstanceID)
	if err != nil {
		return ServerImportResult{}, err
	}
	if found {
		return service.serverImportResult(ctx, created)
	}
	return service.createPreparedServerSource(
		ctx, prepared, targetPlatformInstanceID, tagIDs, assignedByUserID,
	)
}

func (service *Service) createPreparedServerSource(
	ctx context.Context,
	prepared preparedServerSource,
	targetPlatformInstanceID string,
	tagIDs []string,
	assignedByUserID string,
) (ServerImportResult, error) {
	if len(tagIDs) > 0 {
		ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: assignedByUserID})
	}
	created, err := service.create(ctx, CreateRequest{
		UploadID: prepared.uploadID, TargetPlatformInstanceID: targetPlatformInstanceID,
		MetadataProvider: "NONE", ContentMode: prepared.contentMode, TagIDs: tagIDs,
		reviewHandoffKind: prepared.reviewHandoffKind(),
	}, nil)
	if err != nil {
		created, found, lookupErr := service.serverSourceCreation(
			ctx, prepared.uploadID, targetPlatformInstanceID, prepared.contentMode,
		)
		if prepared.idempotent && lookupErr == nil && found {
			return service.serverImportResult(ctx, created)
		}
		service.removeUnusedClonedUpload(ctx, prepared.uploadID)
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
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, 0, ErrInvalid
		}
		if err != nil {
			return nil, nil, 0, fmt.Errorf("read server source blob: %w", err)
		}
		if size != file.SizeBytes {
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
	ownerKind, ownerItemID string,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("libraryimport/server source: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := insertClonedUpload(ctx, transaction, uploadID, sourceType, files, digest, now, totalBytes); err != nil {
		return err
	}
	if ownerKind != "" {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO server_import_upload_owners(upload_session_id,kind,source_item_id)
VALUES(?,?,?)`, uploadID, ownerKind, ownerItemID); err != nil {
			return fmt.Errorf("libraryimport/server source owner: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("libraryimport/server source: %w", err)
	}
	return nil
}

func (service *Service) serverImportResult(ctx context.Context, created Created) (ServerImportResult, error) {
	result, err := librarypersistence.BindSourceResults(service.database).Read(ctx, created)
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("read server source result: %w", err)
	}
	return result, nil
}

func (service *Service) SeedServerReviewMetadata(
	ctx context.Context,
	importItemID string,
	metadata ServerMetadata,
) (int64, []ServerMetadataWarning, error) {
	return service.SeedServerReviewMetadataAtYear(
		ctx, importItemID, metadata, service.now().UTC().Year()+1,
	)
}

func (service *Service) SeedServerReviewMetadataAtYear(
	ctx context.Context,
	importItemID string,
	metadata ServerMetadata,
	maximumYear int,
) (int64, []ServerMetadataWarning, error) {
	version, warnings, err := libraryservice.NewMetadataSeeder(
		librarypersistence.NewMetadata(service.database), service.now,
	).Seed(ctx, importItemID, metadata, maximumYear)
	if err != nil {
		return 0, nil, fmt.Errorf("libraryimport/server metadata: %w", err)
	}
	return version, warnings, nil
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
SELECT source_ref_id,source_kind FROM (
 SELECT id AS source_ref_id,'SERVER_PEGASUS_IMPORT' AS source_kind
 FROM pegasus_import_items
 WHERE library_import_item_id=? AND execution_state='REVIEW_PENDING'
 UNION ALL
 SELECT id AS source_ref_id,'SERVER_EMULATIONSTATION_IMPORT' AS source_kind
 FROM emulationstation_import_items
 WHERE library_import_item_id=? AND execution_state='REVIEW_PENDING'
) ORDER BY source_kind LIMIT 1
`, importItemID, importItemID).Scan(&origin.SourceRefID, &origin.SourceKind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return serverReviewOrigin{}, false, nil
		}
		return serverReviewOrigin{}, false, fmt.Errorf("libraryimport/server review origin: %w", err)
	}
	assetTable := "pegasus_import_item_assets"
	if origin.SourceKind == "SERVER_EMULATIONSTATION_IMPORT" {
		assetTable = "emulationstation_import_item_assets"
	}
	rows, err := transaction.QueryContext(ctx, `
SELECT kind,blob_id,media_type,width_px,height_px
FROM `+assetTable+`
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
	ctx context.Context, transaction *sql.Tx, importItemID, state string, gameID any, now int64,
) error {
	var publishedGameID *string
	if gameID != nil {
		value, ok := gameID.(string)
		if !ok {
			return ErrInvalid
		}
		publishedGameID = &value
	}
	err := librarypersistence.TransitionReviewOwners(ctx, transaction, libraryservice.ReviewOwnerTransition{
		ItemID: importItemID, State: libraryservice.ReviewOwnerState(state), GameID: publishedGameID, NowMS: now,
	})
	if err != nil {
		return fmt.Errorf("libraryimport/transition review owner: %w", err)
	}
	return nil
}

func (service *Service) copyExternalAssets(
	ctx context.Context,
	transaction *sql.Tx,
	gameID string,
	assets []ExternalAsset,
	now int64,
) error {
	for _, asset := range assets {
		var blobID string
		err := transaction.QueryRowContext(
			ctx,
			`SELECT id FROM blobs WHERE id=?`,
			asset.BlobID,
		).Scan(&blobID)
		if err != nil || blobID != asset.BlobID {
			return ErrInvalid
		}
		assetID, _ := uuid.NewV7()
		if _, err := recordstore.CreateGameAssets(ctx, transaction, `
INSERT INTO game_assets(
id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms
)
VALUES(?,?,?, ?,0,?,?,?,?)
`, assetID.String(), gameID, asset.BlobID, asset.Kind,
			asset.WidthPX, asset.HeightPX, asset.MediaType, now); err != nil {
			return fmt.Errorf("libraryimport/server asset: %w", err)
		}
	}
	return nil
}
