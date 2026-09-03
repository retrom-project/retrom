package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/importing"
	"retrom/internal/mediaasset"
	retromruntime "retrom/internal/runtime"
)

const reviewCaptureAfterMS int64 = 5_000

var (
	ErrReviewPreviewUnavailable = errors.New("REVIEW_PREVIEW_UNAVAILABLE")
	ErrReviewCaptureNotAllowed  = errors.New("REVIEW_CAPTURE_NOT_ALLOWED")
	ErrReviewScreenshotInvalid  = errors.New("REVIEW_SCREENSHOT_INVALID")
)

type ReviewPreviewRequest struct {
	ImportItemID       string
	ActorUserID        string
	IdempotencyKey     string
	ClientCapabilities Capabilities
}

type ReviewPreviewCreated struct {
	PreviewID      string `json:"previewId"`
	PlayURL        string `json:"playUrl"`
	CaptureAllowed bool   `json:"captureAllowed"`
	CaptureAfterMS int64  `json:"captureAfterMs"`
	Capability     string `json:"-"`
}

type reviewPreviewSource struct {
	SourceSnapshotID, PlatformInstanceID, PlatformName, PlatformKey        string
	ProviderID, TargetID, TargetContractSHA256, GameCompatibilityLine      string
	BundleSHA256, CoreID, DeliveryProfile                                  string
	Title, ContentKind, ValidationID, ValidationStatus, DependencySnapshot string
	DefaultDOSEntry, SelectedValidationID, DATVersionID                    sql.NullString
	RequiresThreads                                                        bool
}

type reviewPreviewFile struct {
	Role, LogicalName, BlobID string
	VirtualPath               *string
	SortOrder                 int
}

type reviewPreviewContentSet struct {
	BlobID, LogicalName, Format string
	Files                       []reviewPreviewFile
}

// CreateReviewPreview freezes a review-only runtime snapshot without creating a Game or Variant.
// Missing optional runtime dependencies are deliberately omitted, while the source ROM remains required.
func (service *Service) CreateReviewPreview(
	ctx context.Context,
	request ReviewPreviewRequest,
) (ReviewPreviewCreated, error) {
	if !validReviewPreviewRequest(request) {
		return ReviewPreviewCreated{}, ErrReviewPreviewUnavailable
	}
	replay, found, err := service.replayReviewPreview(ctx, request)
	if err != nil {
		return replay, err
	}
	if found {
		return replay, nil
	}
	source, err := service.reviewPreviewSource(ctx, request.ImportItemID)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("load review preview source: %w", err)
	}
	if err := service.validateReviewPreviewSource(ctx, source, request.ClientCapabilities); err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("validate review preview source: %w", err)
	}
	content, err := service.reviewPreviewContent(ctx, source)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("assemble review preview content: %w", err)
	}
	previewID, err := uuid.NewV7()
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("launch/review preview: %w", err)
	}
	capability := service.credentials.Capability(previewID)
	capabilityHash := retromruntime.HashCapability(capability)
	captureAllowed := true
	if err := service.persistReviewPreview(
		ctx, request, source, content, previewID.String(), capabilityHash[:], captureAllowed,
	); err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("persist review preview: %w", err)
	}
	return ReviewPreviewCreated{
		PreviewID: previewID.String(), PlayURL: "/admin/review-previews/" + previewID.String(),
		CaptureAllowed: captureAllowed, CaptureAfterMS: reviewCaptureAfterMS,
		Capability: retromruntime.EncodeCapability(capability),
	}, nil
}

func validReviewPreviewRequest(request ReviewPreviewRequest) bool {
	return request.ImportItemID != "" && request.ActorUserID != "" && request.IdempotencyKey != ""
}

func (service *Service) validateReviewPreviewSource(
	_ context.Context,
	source reviewPreviewSource,
	capabilities Capabilities,
) error {
	target, exists := service.runtimeBuilder.Target(source.ProviderID, source.TargetID)
	if !exists || target.ContractSHA256 != source.TargetContractSHA256 ||
		target.GameCompatibilityLine != source.GameCompatibilityLine {
		return ErrReviewPreviewUnavailable
	}
	if source.RequiresThreads && (!capabilities.SecureContext || !capabilities.CrossOriginIsolated ||
		!capabilities.SharedArrayBuffer) {
		return ErrBlocked
	}
	declared := false
	for _, input := range target.Inputs {
		declared = declared || input.Role == "game"
	}
	if !declared {
		return ErrReviewPreviewUnavailable
	}
	return nil
}

func (service *Service) persistReviewPreview(
	ctx context.Context,
	request ReviewPreviewRequest,
	source reviewPreviewSource,
	content reviewPreviewContentSet,
	previewID string,
	capabilityHash []byte,
	captureAllowed bool,
) error {
	now := service.now().UnixMilli()
	bootstrapExpires := now + int64(5*time.Minute/time.Millisecond)
	hardExpires := now + int64(2*time.Hour/time.Millisecond)
	title := strings.TrimSpace(source.Title)
	if title == "" {
		title = content.LogicalName
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("launch/review preview: %w", err)
	}
	var emulatorGameID any
	if source.DeliveryProfile == "EMULATORJS_CONTENT_V1" || source.DeliveryProfile == "ROM_BLOB_V1" {
		emulatorGameID = max(now, 1)
	}
	defer cleanup.Rollback(transaction)
	_, err = transaction.ExecContext(ctx, `
INSERT INTO review_preview_sessions(id,import_item_id,source_snapshot_id,validation_id,
target_platform_instance_id,provider_id,target_id,target_contract_sha256,game_compatibility_line,bundle_sha256,
actor_user_id,idempotency_key,title,content_kind,
content_blob_id,content_logical_name,content_format,dependency_snapshot_json,default_dos_entry,
emulator_game_id,capture_allowed,credential_sha256,state,bootstrap_expires_at_ms,hard_expires_at_ms,
created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'CREATED',?,?,?,?)
`, previewID, request.ImportItemID, source.SourceSnapshotID, nullableText(source.ValidationID),
		source.PlatformInstanceID, source.ProviderID, source.TargetID, source.TargetContractSHA256,
		source.GameCompatibilityLine, source.BundleSHA256, request.ActorUserID,
		request.IdempotencyKey, title, source.ContentKind,
		content.BlobID, content.LogicalName, content.Format, source.DependencySnapshot,
		nullableSQLString(source.DefaultDOSEntry), emulatorGameID, captureAllowed, capabilityHash,
		bootstrapExpires, hardExpires, now, now)
	if err != nil {
		return fmt.Errorf("create review preview: %w", err)
	}
	for _, file := range content.Files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_preview_files(preview_session_id,role,logical_name,virtual_path,blob_id,sort_order,created_at_ms)
VALUES(?,?,?,?,?,?,?)
`, previewID, file.Role, file.LogicalName, nullableTextPointer(file.VirtualPath), file.BlobID,
			file.SortOrder, now); err != nil {
			return fmt.Errorf("lock review preview dependency: %w", err)
		}
	}
	if source.DeliveryProfile == "ISOLATED_WEB_PROJECT_V1" {
		if err := service.lockIsolatedPreviewBootstrapTicket(
			ctx, transaction, previewID, request.ActorUserID, now,
		); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit review preview: %w", err)
	}
	return nil
}

func (service *Service) replayReviewPreview(
	ctx context.Context,
	request ReviewPreviewRequest,
) (ReviewPreviewCreated, bool, error) {
	var id, itemID string
	var captureAllowed int
	err := service.database.QueryRowContext(ctx, `
SELECT id,import_item_id,capture_allowed
FROM review_preview_sessions
WHERE actor_user_id=? AND idempotency_key=?
`, request.ActorUserID, request.IdempotencyKey).Scan(&id, &itemID, &captureAllowed)
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewPreviewCreated{}, false, nil
	}
	if err != nil {
		return ReviewPreviewCreated{}, false, fmt.Errorf("replay review preview: %w", err)
	}
	if itemID != request.ImportItemID {
		return ReviewPreviewCreated{}, true, ErrReviewPreviewUnavailable
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 {
		return ReviewPreviewCreated{}, true, ErrReviewPreviewUnavailable
	}
	return ReviewPreviewCreated{
		PreviewID: id, PlayURL: "/admin/review-previews/" + id,
		CaptureAllowed: captureAllowed == 1, CaptureAfterMS: reviewCaptureAfterMS,
		Capability: retromruntime.EncodeCapability(service.credentials.Capability(parsed)),
	}, true, nil
}

func (service *Service) reviewPreviewSource(ctx context.Context, itemID string) (reviewPreviewSource, error) {
	var value reviewPreviewSource
	var validationID, validationStatus, dependencySnapshot sql.NullString
	err := service.database.QueryRowContext(ctx, `
SELECT draft.effective_source_snapshot_id,draft.target_platform_instance_id,instance.name,platform.id,
validation.provider_id,validation.target_id,target.target_contract_sha256,target.game_compatibility_line,
provider.bundle_sha256,validation.core_id,binding.delivery_profile,
COALESCE(json_extract(draft.metadata_json,'$.title'),''),snapshot.content_kind,
validation.id,validation.status,validation.dependency_snapshot_json,draft.default_dos_entry,
draft.selected_validation_id,validation.dat_version_id
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
 AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
JOIN platforms platform ON platform.id=instance.platform_id
JOIN import_item_core_validations validation ON validation.id=(
 SELECT candidate.id FROM import_item_core_validations candidate
 WHERE candidate.import_item_id=item.id
 AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
 AND candidate.target_platform_instance_id=draft.target_platform_instance_id
 AND candidate.core_id=instance.default_core_id
 ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
)
JOIN runtime_targets target ON target.provider_id=validation.provider_id AND target.target_id=validation.target_id
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
WHERE item.id=? AND item.state='REVIEW_PENDING'
`, itemID).Scan(
		&value.SourceSnapshotID, &value.PlatformInstanceID, &value.PlatformName, &value.PlatformKey,
		&value.ProviderID, &value.TargetID, &value.TargetContractSHA256, &value.GameCompatibilityLine,
		&value.BundleSHA256, &value.CoreID, &value.DeliveryProfile, &value.Title, &value.ContentKind,
		&validationID, &validationStatus, &dependencySnapshot, &value.DefaultDOSEntry,
		&value.SelectedValidationID, &value.DATVersionID,
	)
	if err != nil || !validationID.Valid || !dependencySnapshot.Valid {
		return reviewPreviewSource{}, ErrReviewPreviewUnavailable
	}
	value.ValidationID = validationID.String
	value.ValidationStatus = validationStatus.String
	value.DependencySnapshot = dependencySnapshot.String
	target, exists := service.runtimeBuilder.Target(value.ProviderID, value.TargetID)
	if !exists {
		return reviewPreviewSource{}, ErrReviewPreviewUnavailable
	}
	value.RequiresThreads = target.Capabilities.RequiresThreads
	return value, nil
}

func (service *Service) reviewPreviewContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	content, err := service.reviewPreviewPrimaryContent(ctx, source)
	if err != nil {
		return reviewPreviewContentSet{}, fmt.Errorf("load primary review content: %w", err)
	}
	if source.ContentKind == onsProjectFormat || source.ContentKind == kirikiriProjectFormat ||
		source.ContentKind == butterscotchProjectFormat || source.ContentKind == tyranoScriptProjectFormat {
		if !validPreviewFileSet(content.LogicalName, content.Files) {
			return reviewPreviewContentSet{}, fmt.Errorf("validate review project names: %w", ErrReviewPreviewUnavailable)
		}
		return content, nil
	}
	content.Files, err = service.reviewPreviewValidationFiles(ctx, source.ValidationID, content.Files)
	if err != nil {
		return reviewPreviewContentSet{}, fmt.Errorf("load validated review dependencies: %w", err)
	}
	if !source.DATVersionID.Valid {
		content.Files, err = reviewPreviewExternalFiles(source.DependencySnapshot, content.Files)
		if err != nil {
			return reviewPreviewContentSet{}, fmt.Errorf("load external review dependencies: %w", err)
		}
	}
	if !validPreviewFileSet(content.LogicalName, content.Files) {
		return reviewPreviewContentSet{}, fmt.Errorf("validate review dependency names: %w", ErrReviewPreviewUnavailable)
	}
	return content, nil
}

func (service *Service) reviewPreviewPrimaryContent(
	ctx context.Context,
	source reviewPreviewSource,
) (reviewPreviewContentSet, error) {
	var content reviewPreviewContentSet
	switch source.ContentKind {
	case "SINGLE_FILE":
		content.Format = "SOURCE_V1"
		if err := service.database.QueryRowContext(ctx, `
SELECT blob_id,logical_name FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role='CONTENT' ORDER BY sort_order,logical_name LIMIT 1
`, source.SourceSnapshotID).Scan(&content.BlobID, &content.LogicalName); err != nil {
			return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
		}
	case "DOS_BUNDLE":
		content.Format, content.LogicalName = "RETROM_DOS_DIRECT_ZIP_V1", "game.zip"
		if err := service.database.QueryRowContext(ctx, `
SELECT blob_id FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND role='DOS_LAUNCH_BUNDLE' AND logical_name='game.zip'
`, source.ValidationID).Scan(&content.BlobID); err != nil {
			return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
		}
	case corevalidation.MultiDiscContentKind:
		content.Format, content.LogicalName = "RETROM_MULTIDISC_M3U_V1", "playlist.m3u"
		if source.ValidationStatus != "READY" {
			return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
		}
		if err := service.database.QueryRowContext(ctx, `
SELECT blob_id FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND role='MULTI_DISC_PLAYLIST' AND logical_name='playlist.m3u'
`, source.ValidationID).Scan(&content.BlobID); err != nil {
			return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
		}
		files, err := service.reviewPreviewDiscFiles(ctx, source.SourceSnapshotID)
		if err != nil {
			return reviewPreviewContentSet{}, err
		}
		content.Files = files
	case onsProjectFormat:
		return service.reviewPreviewONSContent(ctx, source)
	case kirikiriProjectFormat:
		return service.reviewPreviewKiriKiriContent(ctx, source)
	case butterscotchProjectFormat:
		return service.reviewPreviewButterscotchContent(ctx, source)
	case tyranoScriptProjectFormat:
		return service.reviewPreviewTyranoScriptContent(ctx, source)
	default:
		return reviewPreviewContentSet{}, ErrReviewPreviewUnavailable
	}
	return content, nil
}

func (service *Service) reviewPreviewDiscFiles(
	ctx context.Context,
	sourceSnapshotID string,
) ([]reviewPreviewFile, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT logical_name,blob_id,sort_order FROM import_item_source_snapshot_files
WHERE source_snapshot_id=? AND role='DISC' ORDER BY sort_order,logical_name
`, sourceSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("review preview discs: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]reviewPreviewFile, 0, 8)
	for rows.Next() {
		var name, blobID string
		var order int
		if err := rows.Scan(&name, &blobID, &order); err != nil || len(files) >= 8 {
			return nil, ErrReviewPreviewUnavailable
		}
		virtualPath := "/" + name
		files = append(files, reviewPreviewFile{
			Role: "DISC", LogicalName: name, BlobID: blobID,
			VirtualPath: &virtualPath, SortOrder: order,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review preview discs: %w", err)
	}
	if len(files) < 2 {
		return nil, ErrReviewPreviewUnavailable
	}
	return files, nil
}

func (service *Service) reviewPreviewValidationFiles(
	ctx context.Context,
	validationID string,
	files []reviewPreviewFile,
) ([]reviewPreviewFile, error) {
	dependencyRows, err := service.database.QueryContext(ctx, `
SELECT role,logical_name,blob_id,sort_order FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND role IN ('PARENT','BIOS_BUNDLE')
ORDER BY role,sort_order,logical_name
`, validationID)
	if err != nil {
		return nil, fmt.Errorf("review preview dependencies: %w", err)
	}
	defer func() { cleanup.Error("close", dependencyRows.Close()) }()
	for dependencyRows.Next() {
		var file reviewPreviewFile
		if err := dependencyRows.Scan(&file.Role, &file.LogicalName, &file.BlobID, &file.SortOrder); err != nil ||
			len(files) >= 16 || !validPreviewLogicalName(file.LogicalName) {
			return nil, ErrReviewPreviewUnavailable
		}
		files = append(files, file)
	}
	if err := dependencyRows.Err(); err != nil {
		return nil, fmt.Errorf("review preview dependencies: %w", err)
	}
	return files, nil
}

func reviewPreviewExternalFiles(
	dependencySnapshot string,
	files []reviewPreviewFile,
) ([]reviewPreviewFile, error) {
	snapshot, err := corevalidation.ParseSnapshot(dependencySnapshot)
	if err != nil {
		return nil, ErrReviewPreviewUnavailable
	}
	for _, dependency := range snapshot.BIOS {
		if !availableReviewPreviewExternal(dependency) {
			continue
		}
		if len(files) >= 16 || !validPreviewLogicalName(dependency.LogicalName) {
			return nil, ErrReviewPreviewUnavailable
		}
		files = append(files, reviewPreviewFile{
			Role: "EXTERNAL_FILE", LogicalName: dependency.LogicalName, BlobID: *dependency.BlobID,
			VirtualPath: dependency.EmulatorPath, SortOrder: len(files),
		})
	}
	return files, nil
}

func availableReviewPreviewExternal(dependency corevalidation.BIOSDependency) bool {
	if dependency.DeliveryKind != "EXTERNAL_FILE" || dependency.EmulatorPath == nil || dependency.BlobID == nil ||
		dependency.InstallationStatus == nil {
		return false
	}
	return *dependency.InstallationStatus == "MATCHED" || *dependency.InstallationStatus == "HASH_WARNING"
}

func validPreviewLogicalName(value string) bool {
	return value != "" && len(value) <= 255 && path.Base(value) == value && value != "." && value != ".." &&
		!strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}

func validPreviewFileSet(contentName string, files []reviewPreviewFile) bool {
	seenNames := map[string]struct{}{importing.ASCIICaseFold(contentName): {}}
	seenPaths := make(map[string]struct{})
	for _, file := range files {
		if file.Role == "PROJECT_FILE" {
			if _, err := importing.ValidateLogicalPath(file.LogicalName); err != nil || file.VirtualPath != nil {
				return false
			}
		} else if !validPreviewLogicalName(file.LogicalName) {
			return false
		}
		name := importing.ASCIICaseFold(file.LogicalName)
		if _, duplicate := seenNames[name]; duplicate {
			return false
		}
		seenNames[name] = struct{}{}
		if file.VirtualPath != nil {
			if _, duplicate := seenPaths[*file.VirtualPath]; duplicate {
				return false
			}
			seenPaths[*file.VirtualPath] = struct{}{}
		}
	}
	return true
}

func (service *Service) ReviewPreviewConfig(ctx context.Context, previewID, capability string) (Config, error) {
	var source providerConfigSource
	var captureAllowed int
	err := service.database.QueryRowContext(ctx, `
SELECT preview.credential_sha256,preview.state,preview.provider_id,preview.target_id,
 preview.target_contract_sha256,preview.game_compatibility_line,preview.bundle_sha256,
 binding.core_id,binding.delivery_profile,'REVIEW_PREVIEW',preview.title,instance.name,
 '/admin/reviews/' || preview.import_item_id,preview.content_kind,preview.dependency_snapshot_json,'',
 NULL,NULL,preview.default_dos_entry,NULL,NULL,NULL,NULL,
 preview.bootstrap_expires_at_ms,preview.hard_expires_at_ms,NULL,0,preview.capture_allowed
FROM review_preview_sessions preview
JOIN platform_instances instance ON instance.id=preview.target_platform_instance_id
JOIN runtime_target_bindings binding ON binding.provider_id=preview.provider_id AND binding.target_id=preview.target_id
WHERE preview.id=?
`, previewID).Scan(
		&source.credentialHash, &source.state, &source.providerID, &source.targetID,
		&source.targetDigest, &source.gameLine, &source.bundleDigest, &source.coreID,
		&source.delivery, &source.purpose, &source.title, &source.platformName, &source.returnTo,
		&source.contentKind, &source.dependencyJSON, &source.compatibility, &source.variantID, &source.saveID,
		&source.dosEntry, &source.validationID, &source.netplayID, &source.netplayPlayer,
		&source.netplayRoom, &source.bootstrapEnd, &source.hardEnd, &source.idleEnd,
		&source.initialDisc, &captureAllowed,
	)
	if err != nil || service.runtimeBuilder == nil ||
		!retromruntime.MatchesCapability(capability, source.credentialHash) ||
		!validConfigLifetime(source.state, source.bootstrapEnd, source.hardEnd, source.idleEnd, service.now().UnixMilli()) {
		return Config{}, ErrCredential
	}
	if err := service.activateReviewPreview(ctx, previewID, source.state); err != nil {
		return Config{}, err
	}
	return service.providerEnvelope(ctx, previewID, capability, source)
}

func (service *Service) activateReviewPreview(ctx context.Context, previewID, state string) error {
	if state != "CREATED" {
		return nil
	}
	now := service.now().UnixMilli()
	if _, err := service.database.ExecContext(ctx, `
UPDATE review_preview_sessions SET state='ACTIVE',activated_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=? AND state='CREATED'
`, now, now, previewID); err != nil {
		return fmt.Errorf("activate review preview: %w", err)
	}
	return nil
}

type ReviewScreenshot struct {
	ID, ImportItemID, ValidationID             string
	ProviderID, TargetID, TargetContractSHA256 string
	WidthPX, HeightPX, CapturedAtMS            int64
}

type reviewScreenshotImage struct {
	Blob  blobstore.Metadata
	Image mediaasset.Image
}

type reviewScreenshotTarget struct {
	ItemID, SourceSnapshotID, ValidationID     string
	ProviderID, TargetID, TargetContractSHA256 string
}

func (service *Service) StoreReviewScreenshot(
	ctx context.Context,
	previewID, capability string,
	reader io.Reader,
) (ReviewScreenshot, error) {
	if service.blobs == nil {
		return ReviewScreenshot{}, ErrReviewScreenshotInvalid
	}
	if err := service.authorizeReviewScreenshot(ctx, previewID, capability); err != nil {
		return ReviewScreenshot{}, err
	}
	image, err := service.inspectReviewScreenshot(reader)
	if err != nil {
		return ReviewScreenshot{}, err
	}
	return service.persistReviewScreenshot(ctx, previewID, capability, image)
}

func (service *Service) inspectReviewScreenshot(reader io.Reader) (reviewScreenshotImage, error) {
	metadata, err := service.blobs.Put(reader)
	if err != nil {
		return reviewScreenshotImage{}, ErrReviewScreenshotInvalid
	}
	file, err := os.Open(metadata.Path)
	if err != nil {
		return reviewScreenshotImage{}, ErrReviewScreenshotInvalid
	}
	image, inspectErr := mediaasset.InspectImage(file, metadata.Size)
	cleanup.Error("close", file.Close())
	if inspectErr != nil || image.MediaType != "image/png" && image.MediaType != "image/jpeg" {
		return reviewScreenshotImage{}, ErrReviewScreenshotInvalid
	}
	return reviewScreenshotImage{Blob: metadata, Image: image}, nil
}

func (service *Service) persistReviewScreenshot(
	ctx context.Context,
	previewID, capability string,
	image reviewScreenshotImage,
) (ReviewScreenshot, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("review screenshot: %w", err)
	}
	defer cleanup.Rollback(transaction)
	target, err := service.reviewScreenshotTarget(ctx, transaction, previewID, capability)
	if err != nil {
		return ReviewScreenshot{}, err
	}
	now := service.now().UnixMilli()
	blobID, err := blobstore.EnsureRecord(ctx, transaction, image.Blob, image.Image.MediaType, now)
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("register review screenshot: %w", err)
	}
	screenshotID, err := uuid.NewV7()
	if err != nil {
		return ReviewScreenshot{}, fmt.Errorf("review screenshot: %w", err)
	}
	if err := insertReviewScreenshot(
		ctx, transaction, screenshotID.String(), previewID, target, blobID, image.Image, now,
	); err != nil {
		return ReviewScreenshot{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ReviewScreenshot{}, fmt.Errorf("commit review screenshot: %w", err)
	}
	return ReviewScreenshot{
		ID: screenshotID.String(), ImportItemID: target.ItemID, ValidationID: target.ValidationID,
		ProviderID: target.ProviderID, TargetID: target.TargetID,
		TargetContractSHA256: target.TargetContractSHA256,
		WidthPX:              image.Image.WidthPX, HeightPX: image.Image.HeightPX,
		CapturedAtMS: now,
	}, nil
}

func (service *Service) reviewScreenshotTarget(
	ctx context.Context,
	transaction *sql.Tx,
	previewID, capability string,
) (reviewScreenshotTarget, error) {
	var target reviewScreenshotTarget
	var credentialHash []byte
	var state string
	var hardExpires int64
	var captureAllowed int
	err := transaction.QueryRowContext(ctx, `
SELECT preview.credential_sha256,preview.state,preview.hard_expires_at_ms,preview.import_item_id,
preview.source_snapshot_id,preview.validation_id,preview.provider_id,preview.target_id,
preview.target_contract_sha256,preview.capture_allowed
FROM review_preview_sessions preview
JOIN import_items item ON item.id=preview.import_item_id AND item.state='REVIEW_PENDING'
JOIN review_drafts draft ON draft.import_item_id=item.id
 AND draft.effective_source_snapshot_id=preview.source_snapshot_id
 AND draft.target_platform_instance_id=preview.target_platform_instance_id
JOIN import_item_core_validations validation ON validation.id=preview.validation_id
 AND validation.import_item_id=preview.import_item_id
 AND validation.source_snapshot_id=preview.source_snapshot_id
 AND validation.target_platform_instance_id=preview.target_platform_instance_id
 AND validation.provider_id=preview.provider_id AND validation.target_id=preview.target_id
 AND validation.target_contract_sha256=preview.target_contract_sha256
 AND validation.id=(
  SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=preview.import_item_id
   AND candidate.source_snapshot_id=preview.source_snapshot_id
   AND candidate.target_platform_instance_id=preview.target_platform_instance_id
   AND candidate.provider_id=preview.provider_id AND candidate.target_id=preview.target_id
   AND candidate.target_contract_sha256=preview.target_contract_sha256
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
 )
WHERE preview.id=?
	`, previewID).Scan(
		&credentialHash, &state, &hardExpires, &target.ItemID, &target.SourceSnapshotID,
		&target.ValidationID, &target.ProviderID, &target.TargetID,
		&target.TargetContractSHA256, &captureAllowed,
	)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return reviewScreenshotTarget{}, ErrCredential
	}
	if captureAllowed != 1 {
		return reviewScreenshotTarget{}, ErrReviewCaptureNotAllowed
	}
	return target, nil
}

func insertReviewScreenshot(
	ctx context.Context,
	transaction *sql.Tx,
	screenshotID, previewID string,
	target reviewScreenshotTarget,
	blobID string,
	image mediaasset.Image,
	now int64,
) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO review_runtime_screenshots(id,import_item_id,preview_session_id,source_snapshot_id,
validation_id,provider_id,target_id,target_contract_sha256,blob_id,media_type,width_px,height_px,captured_after_ms,
captured_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,5000,?,?,?)
ON CONFLICT(import_item_id,validation_id) DO UPDATE SET
id=excluded.id,preview_session_id=excluded.preview_session_id,source_snapshot_id=excluded.source_snapshot_id,
provider_id=excluded.provider_id,target_id=excluded.target_id,
target_contract_sha256=excluded.target_contract_sha256,
blob_id=excluded.blob_id,media_type=excluded.media_type,
width_px=excluded.width_px,height_px=excluded.height_px,captured_after_ms=excluded.captured_after_ms,
captured_at_ms=excluded.captured_at_ms,updated_at_ms=excluded.updated_at_ms
	`, screenshotID, target.ItemID, previewID, target.SourceSnapshotID, target.ValidationID,
		target.ProviderID, target.TargetID, target.TargetContractSHA256, blobID,
		image.MediaType, image.WidthPX, image.HeightPX, now, now, now)
	if err != nil {
		return fmt.Errorf("store review screenshot: %w", err)
	}
	return nil
}

func (service *Service) authorizeReviewScreenshot(ctx context.Context, previewID, capability string) error {
	var credentialHash []byte
	var state string
	var hardExpires int64
	var captureAllowed int
	err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms,capture_allowed
FROM review_preview_sessions WHERE id=?
`, previewID).Scan(&credentialHash, &state, &hardExpires, &captureAllowed)
	if err != nil || !reviewPreviewCredential(service.now().UnixMilli(), capability, credentialHash, state, hardExpires) {
		return ErrCredential
	}
	if captureAllowed != 1 {
		return ErrReviewCaptureNotAllowed
	}
	return nil
}
