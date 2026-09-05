package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"retrom/internal/contentcapability"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	retromruntime "retrom/internal/runtime"
)

// Create resolves a published Product Core to its active Provider Target and
// freezes that identity before any Provider code is loaded by the browser.
func (service *Service) Create(ctx context.Context, profileID string, request CreateRequest) (Created, error) {
	if !validCreateRequest(profileID, request) {
		return Created{}, ErrBlocked
	}
	coreID := ""
	if request.CoreID != nil {
		coreID = *request.CoreID
	}
	result, err := service.selectLaunchVariant(ctx, profileID, request, coreID)
	if err != nil {
		return Created{}, err
	}
	if result.retry != nil {
		return *result.retry, nil
	}
	preparation, err := service.prepareLaunch(ctx, request, result.selection)
	if err != nil {
		return Created{}, err
	}
	return service.persistLaunch(ctx, profileID, request, result.selection, preparation)
}

func validCreateRequest(profileID string, request CreateRequest) bool {
	return profileID != "" && request.GameID != "" && validReturnTo(request.ReturnTo, request.GameID, request.SaveStateID)
}

type launchPreparation struct {
	contentPlan      launchContentPlan
	selectedDOSEntry *string
	initialDiscIndex int64
}

type launchSelection struct {
	variantID, selectedCore                              string
	providerID, targetID, bundleSHA256                   string
	gameID, contentLogicalName, contentKind              string
	platformID, platformName, gameTitle, deliveryProfile string
	dependencySnapshotJSON, compatibilityCode            string
	contentPolicy                                        contentcapability.Policy
	datID                                                sql.NullString
	savedDOSEntry                                        sql.NullString
	savedDiscIndex                                       sql.NullInt64
}

type launchSelectionResult struct {
	selection launchSelection
	retry     *Created
}

func (service *Service) prepareLaunch(
	ctx context.Context,
	request CreateRequest,
	selection launchSelection,
) (launchPreparation, error) {
	target, exists := service.runtimeBuilder.Target(selection.providerID, selection.targetID)
	if !exists || !validThreadCapabilities(target.Capabilities.RequiresThreads, request.ClientCapabilities) {
		return launchPreparation{}, ErrBlocked
	}
	selectedDOSEntry := request.DOSEntry
	if request.SaveStateID != nil && selection.savedDOSEntry.Valid {
		selectedDOSEntry = &selection.savedDOSEntry.String
	}
	if err := service.validateDOSEntry(ctx, selection.variantID, selectedDOSEntry); err != nil {
		return launchPreparation{}, err
	}
	plan, err := service.buildProviderContentPlan(ctx, selection)
	if err != nil || plan.ContentKind != selection.contentKind {
		return launchPreparation{}, ErrBlocked
	}
	initialDiscIndex, err := launchInitialDiscIndex(
		selection.contentKind, request.SaveStateID != nil, selection.savedDiscIndex, len(plan.Discs),
	)
	if err != nil {
		return launchPreparation{}, err
	}
	return launchPreparation{
		contentPlan: plan, selectedDOSEntry: selectedDOSEntry, initialDiscIndex: initialDiscIndex,
	}, nil
}

func validThreadCapabilities(requiresThreads bool, capabilities Capabilities) bool {
	return !requiresThreads || capabilities.SecureContext &&
		capabilities.CrossOriginIsolated && capabilities.SharedArrayBuffer
}

func (service *Service) buildProviderContentPlan(
	ctx context.Context,
	selection launchSelection,
) (launchContentPlan, error) {
	switch selection.deliveryProfile {
	case "EMULATORJS_CONTENT":
		return service.buildEmulatorContentPlan(ctx, selection)
	case "ROM_BLOB":
		return service.buildSingleContentPlan(ctx, selection, "SOURCE_V1")
	case "FILE_TREE_PROJECT":
		if selection.contentKind == rpgProjectFormat {
			return service.buildRPGProductContentPlan(ctx, selection)
		}
		return service.buildProjectContentPlan(ctx, selection)
	case "SEEKABLE_PROJECT_ARCHIVE":
		return service.buildRPGProductContentPlan(ctx, selection)
	case "ISOLATED_WEB_PROJECT":
		if selection.contentKind == rpgProjectFormat {
			return service.buildRPGProductContentPlan(ctx, selection)
		}
		return service.buildProjectContentPlan(ctx, selection)
	default:
		return launchContentPlan{}, ErrBlocked
	}
}

func (service *Service) buildSingleContentPlan(
	ctx context.Context,
	selection launchSelection,
	format string,
) (launchContentPlan, error) {
	var file lockedContentFile
	file.Format = format
	if err := service.database.QueryRowContext(ctx, `
SELECT file.blob_id,file.logical_name
FROM game_files file
WHERE file.game_id=? AND file.role='CONTENT'
ORDER BY file.sort_order,file.logical_name LIMIT 1
`, selection.gameID).Scan(&file.BlobID, &file.LogicalName); err != nil {
		return launchContentPlan{}, ErrBlocked
	}
	return launchContentPlan{ContentKind: selection.contentKind, Files: []lockedContentFile{file}}, nil
}

func (service *Service) buildProjectContentPlan(
	ctx context.Context,
	selection launchSelection,
) (launchContentPlan, error) {
	files, err := service.projectProductFiles(ctx, selection.gameID, selection.contentKind, 100_000)
	if err != nil || len(files) == 0 {
		return launchContentPlan{}, ErrBlocked
	}
	return launchContentPlan{ContentKind: selection.contentKind, Files: files}, nil
}

func (service *Service) buildEmulatorContentPlan(
	ctx context.Context,
	selection launchSelection,
) (launchContentPlan, error) {
	if selection.contentKind == corevalidation.MultiDiscContentKind {
		return service.buildMultiDiscLaunchContentPlan(
			ctx, selection.variantID, selection.gameID, selection.dependencySnapshotJSON,
		)
	}
	if selection.contentKind == "DOS_BUNDLE" {
		var file lockedContentFile
		file.LogicalName, file.Format = "game.zip", "RETROM_DOS_DIRECT_ZIP_V1"
		if err := service.database.QueryRowContext(ctx, `
SELECT blob_id FROM variant_files
WHERE game_variant_id=? AND role='DOS_LAUNCH_BUNDLE' AND logical_name='game.zip'
`, selection.variantID).Scan(&file.BlobID); err != nil {
			return launchContentPlan{}, ErrBlocked
		}
		return launchContentPlan{ContentKind: selection.contentKind, Files: []lockedContentFile{file}}, nil
	}
	return service.buildSingleContentPlan(ctx, selection, "SOURCE_V1")
}

func (service *Service) selectLaunchVariant(
	ctx context.Context,
	profileID string,
	request CreateRequest,
	coreID string,
) (launchSelectionResult, error) {
	if request.SaveStateID != nil {
		selection, err := service.selectSavedLaunchVariant(ctx, profileID, request, coreID)
		return launchSelectionResult{selection: selection}, err
	}
	return service.selectCurrentLaunchVariant(ctx, profileID, request, coreID)
}

const launchSelectionColumns = `
variant.id,variant.core_id,variant.provider_id,variant.target_id,
provider.bundle_sha256,game.id,game.content_kind,
platform.id,platform.name,game.title,binding.delivery_profile,
` + contentcapability.BindingPolicySQL + `,
variant.dependency_snapshot_json,
variant.compatibility_code,variant.dat_version_id,
COALESCE((SELECT file.logical_name FROM game_files file
 WHERE file.game_id=game.id
 AND file.role IN ('CONTENT','DISC','DOS_SOURCE','PROJECT_FILE')
 ORDER BY CASE file.role WHEN 'CONTENT' THEN 0 WHEN 'DISC' THEN 1 WHEN 'DOS_SOURCE' THEN 2 ELSE 3 END,
 file.sort_order,file.logical_name LIMIT 1),'')`

func scanLaunchSelection(row *sql.Row, selection *launchSelection) error {
	destinations := []any{
		&selection.variantID, &selection.selectedCore,
		&selection.providerID, &selection.targetID, &selection.bundleSHA256, &selection.gameID,
		&selection.contentKind, &selection.platformID, &selection.platformName, &selection.gameTitle,
		&selection.deliveryProfile, &selection.contentPolicy, &selection.dependencySnapshotJSON,
		&selection.compatibilityCode, &selection.datID, &selection.contentLogicalName,
	}
	if err := row.Scan(destinations...); err != nil {
		return fmt.Errorf("scan launch selection: %w", err)
	}
	return nil
}

func (service *Service) selectSavedLaunchVariant(
	ctx context.Context,
	profileID string,
	request CreateRequest,
	coreID string,
) (launchSelection, error) {
	if request.DOSEntry != nil {
		return launchSelection{}, ErrBlocked
	}
	var selection launchSelection
	query := `SELECT ` + launchSelectionColumns + `,save.dos_entry_path,save.disc_index
FROM save_states save
JOIN games game ON game.id=save.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN game_variants variant ON variant.game_id=game.id
JOIN runtime_targets target ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
WHERE save.id=? AND save.game_id=? AND save.profile_id=? AND save.deleted_at_ms IS NULL
	 AND game.status='PUBLISHED' AND instance.enabled=1 AND variant.status='READY'
	 AND binding.core_id=variant.core_id AND binding.launch_policy!='DISABLED'
	 AND variant.core_id=CASE WHEN ?='' THEN instance.default_core_id ELSE ? END
	 AND target.checkpoint_json IS NOT NULL AND EXISTS(
	   SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
	   WHERE readable.type='text' AND readable.value=save.checkpoint_format
	 )`
	row := service.database.QueryRowContext(ctx, query, *request.SaveStateID, request.GameID, profileID, coreID, coreID)
	destinations := []any{
		&selection.variantID, &selection.selectedCore,
		&selection.providerID, &selection.targetID, &selection.bundleSHA256, &selection.gameID,
		&selection.contentKind, &selection.platformID, &selection.platformName, &selection.gameTitle,
		&selection.deliveryProfile, &selection.contentPolicy, &selection.dependencySnapshotJSON,
		&selection.compatibilityCode, &selection.datID, &selection.contentLogicalName,
		&selection.savedDOSEntry, &selection.savedDiscIndex,
	}
	if err := row.Scan(destinations...); err != nil {
		if errors.Is(err, sql.ErrNoRows) && service.saveStateRuntimeIncompatible(
			ctx, profileID, request.GameID, *request.SaveStateID,
		) {
			return launchSelection{}, ErrSaveIncompatible
		}
		return launchSelection{}, ErrBlocked
	}
	if request.CoreID != nil && coreID != selection.selectedCore {
		return launchSelection{}, ErrBlocked
	}
	return selection, nil
}

func (service *Service) saveStateRuntimeIncompatible(
	ctx context.Context,
	profileID, gameID, saveStateID string,
) bool {
	var unreadable int
	err := service.database.QueryRowContext(ctx, `
SELECT NOT EXISTS(
	SELECT 1
	FROM game_variants variant
	JOIN runtime_targets target ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
	WHERE variant.game_id=save.game_id AND variant.status='READY'
	AND target.checkpoint_json IS NOT NULL
	AND EXISTS(SELECT 1 FROM json_each(target.checkpoint_json,'$.readFormats') readable
	 WHERE readable.type='text' AND readable.value=save.checkpoint_format)
)
FROM save_states save
JOIN games game ON game.id=save.game_id
WHERE save.id=? AND save.profile_id=? AND save.game_id=? AND save.deleted_at_ms IS NULL
 AND game.status='PUBLISHED'
`, saveStateID, profileID, gameID).Scan(&unreadable)
	return err == nil && unreadable == 1
}

func (service *Service) selectCurrentLaunchVariant(
	ctx context.Context,
	profileID string,
	request CreateRequest,
	coreID string,
) (launchSelectionResult, error) {
	var selection launchSelection
	query := `SELECT ` + launchSelectionColumns + `
FROM games game
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN game_variants variant ON variant.game_id=game.id
JOIN runtime_targets target ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id AND binding_platform.core_id=variant.core_id
JOIN runtime_binding_content_kinds binding_kind ON binding_kind.binding_id=binding.binding_id
	 AND binding_kind.content_kind=game.content_kind
WHERE game.id=? AND game.status='PUBLISHED' AND instance.enabled=1 AND variant.status='READY'
 AND binding.launch_policy!='DISABLED'
 AND variant.core_id=CASE WHEN ?='' THEN instance.default_core_id ELSE ? END
LIMIT 1`
	if err := scanLaunchSelection(
		service.database.QueryRowContext(ctx, query, request.GameID, coreID, coreID),
		&selection,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			created, ensureErr := service.ensureVariant(ctx, profileID, request, coreID, true)
			return launchSelectionResult{retry: &created}, ensureErr
		}
		return launchSelectionResult{}, ErrBlocked
	}
	fresh, err := service.currentBIOSMatchesDependencySnapshot(ctx, selection)
	if err != nil {
		return launchSelectionResult{}, ErrBlocked
	}
	if !fresh {
		created, ensureErr := service.ensureVariant(ctx, profileID, request, coreID, true)
		return launchSelectionResult{retry: &created}, ensureErr
	}
	return launchSelectionResult{selection: selection}, nil
}

func (service *Service) currentBIOSMatchesDependencySnapshot(
	ctx context.Context,
	selection launchSelection,
) (bool, error) {
	if selection.compatibilityCode == reviewScreenshotOverrideCode {
		return true, nil
	}
	// DAT-backed variants are invalidated by the transactional DAT/BIOS
	// replacement flows; their typed Arcade closure is not a static BIOS snapshot.
	if selection.datID.Valid {
		return service.currentDATBIOSMatchesLockedFiles(ctx, selection)
	}
	current, _, _, err := corevalidation.ResolveBIOS(
		ctx, service.database, selection.providerID, selection.targetID, selection.contentLogicalName,
	)
	if err != nil {
		return false, fmt.Errorf("launch/resolve current BIOS: %w", err)
	}
	locked, err := corevalidation.ParseRuntimeBIOSDependencies(selection.dependencySnapshotJSON)
	if err != nil {
		// Provider-only project targets can legitimately carry an opaque empty
		// dependency snapshot. They are fresh when the Host has no BIOS facts.
		if len(current.BIOS) == 0 {
			return true, nil
		}
		return false, fmt.Errorf("launch/parse locked BIOS dependencies: %w", err)
	}
	current.BIOS = append([]corevalidation.BIOSDependency(nil), current.BIOS...)
	lockedSnapshot := corevalidation.Snapshot{
		SchemaVersion: corevalidation.SnapshotSchemaVersion, Kind: corevalidation.SnapshotKindStatic,
		BIOS: append([]corevalidation.BIOSDependency(nil), locked...),
	}
	currentDigest, err := corevalidation.BIOSDependencyDigest(current)
	if err != nil {
		return false, fmt.Errorf("launch/digest current BIOS dependencies: %w", err)
	}
	lockedDigest, err := corevalidation.BIOSDependencyDigest(lockedSnapshot)
	if err != nil {
		return false, fmt.Errorf("launch/digest locked BIOS dependencies: %w", err)
	}
	return currentDigest == lockedDigest, nil
}

func (service *Service) currentDATBIOSMatchesLockedFiles(
	ctx context.Context,
	selection launchSelection,
) (bool, error) {
	current, _, _, err := service.resolveVariantBIOS(
		ctx, service.database, selection.variantID, selection.gameID,
		selection.providerID, selection.targetID, selection.contentLogicalName, selection.datID,
	)
	if err != nil {
		return false, err
	}
	type lockedBIOS struct{ logicalName, blobID string }
	wanted := make([]lockedBIOS, 0, len(current.BIOS))
	for _, dependency := range current.BIOS {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
			wanted = append(wanted, lockedBIOS{dependency.LogicalName, *dependency.BlobID})
		}
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT logical_name,blob_id FROM variant_files
WHERE game_variant_id=? AND role='BIOS_BUNDLE' ORDER BY sort_order,logical_name
`, selection.variantID)
	if err != nil {
		return false, fmt.Errorf("launch/query locked DAT BIOS files: %w", err)
	}
	defer func() { cleanup.Error("close current DAT BIOS files", rows.Close()) }()
	locked := make([]lockedBIOS, 0, len(wanted))
	for rows.Next() {
		var file lockedBIOS
		if err := rows.Scan(&file.logicalName, &file.blobID); err != nil {
			return false, fmt.Errorf("launch/scan locked DAT BIOS file: %w", err)
		}
		locked = append(locked, file)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("launch/iterate locked DAT BIOS files: %w", err)
	}
	return slices.Equal(wanted, locked), nil
}

func (service *Service) validateDOSEntry(
	ctx context.Context,
	variantID string,
	selectedDOSEntry *string,
) error {
	if selectedDOSEntry == nil {
		return nil
	}
	var directLaunchSafe int
	err := service.database.QueryRowContext(ctx, `
SELECT entry.direct_launch_safe
FROM game_variants variant
JOIN dos_entries entry ON entry.game_id=variant.game_id
WHERE variant.id=? AND entry.normalized_path=? AND entry.enabled=1
`, variantID, *selectedDOSEntry).Scan(&directLaunchSafe)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDOSEntryMissing
	}
	if err != nil {
		return fmt.Errorf("launch/service: %w", err)
	}
	if directLaunchSafe != 1 {
		return ErrDOSEntryUnsafe
	}
	return nil
}

func launchInitialDiscIndex(contentKind string, restoring bool, saved sql.NullInt64, discCount int) (int64, error) {
	if contentKind != corevalidation.MultiDiscContentKind {
		if saved.Valid {
			return 0, ErrBlocked
		}
		return 0, nil
	}
	if !restoring {
		if saved.Valid {
			return 0, ErrBlocked
		}
		return 0, nil
	}
	if !saved.Valid || saved.Int64 < 0 || saved.Int64 >= int64(discCount) {
		return 0, ErrBlocked
	}
	return saved.Int64, nil
}

func (service *Service) persistLaunch(
	ctx context.Context,
	profileID string,
	request CreateRequest,
	selection launchSelection,
	preparation launchPreparation,
) (Created, error) {
	launchID, err := uuid.NewV7()
	if err != nil {
		return Created{}, fmt.Errorf("launch/service: %w", err)
	}
	capability := service.credentials.Capability(launchID)
	capabilityHash := retromruntime.HashCapability(capability)
	now := service.now().UnixMilli()
	bootstrapExpires := now + int64(5*time.Minute/time.Millisecond)
	hardExpires := now + int64(24*time.Hour/time.Millisecond)
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("launch/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err = transaction.ExecContext(ctx, `
INSERT INTO launch_sessions(
 id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,
 content_kind,dependency_snapshot_json,compatibility_code,
 save_state_id,dos_entry_path,initial_disc_index,return_to,credential_sha256,state,
 bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'CREATED',?,?,?,?)
`, launchID.String(), profileID, request.GameID, selection.selectedCore,
		selection.providerID, selection.targetID, selection.bundleSHA256,
		selection.contentKind, selection.dependencySnapshotJSON, selection.compatibilityCode,
		request.SaveStateID, preparation.selectedDOSEntry, preparation.initialDiscIndex,
		request.ReturnTo, capabilityHash[:], bootstrapExpires, hardExpires, now, now); err != nil {
		return Created{}, fmt.Errorf("create launch session: %w", err)
	}
	if selection.deliveryProfile == "ISOLATED_WEB_PROJECT" {
		if err := service.lockIsolatedLaunchBootstrapTicket(ctx, transaction, launchID.String(), profileID, now); err != nil {
			return Created{}, err
		}
	}
	for _, file := range preparation.contentPlan.Files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
VALUES(?,?,?,?,?)
`, launchID.String(), file.LogicalName, file.BlobID, file.Format, now); err != nil {
			return Created{}, fmt.Errorf("lock launch content: %w", err)
		}
	}
	for _, disc := range preparation.contentPlan.Discs {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_external_files(launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind)
VALUES(?,?,?,?,?,'DISC')
`, launchID.String(), disc.VirtualPath, disc.LogicalName, disc.BlobID, now); err != nil {
			return Created{}, fmt.Errorf("lock launch disc: %w", err)
		}
	}
	if selection.deliveryProfile == "EMULATORJS_CONTENT" {
		if err := service.lockExternalBIOS(
			ctx, transaction, launchID.String(), selection.variantID, now, false,
		); err != nil {
			return Created{}, err
		}
	}
	if err := service.lockVariantBundleFiles(
		ctx, transaction, launchID.String(), selection.variantID, now,
	); err != nil {
		return Created{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("commit launch session: %w", err)
	}
	return Created{
		LaunchID: launchID.String(), PlayURL: "/play/" + launchID.String(), Warnings: []string{},
		BootstrapExpiresAtMS: bootstrapExpires, HardExpiresAtMS: hardExpires,
		Capability: retromruntime.EncodeCapability(capability),
	}, nil
}

func (service *Service) lockVariantBundleFiles(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, variantID string,
	now int64,
) error {
	rows, err := transaction.QueryContext(ctx, `
SELECT role,logical_name,blob_id,sort_order
FROM variant_files
WHERE game_variant_id=? AND role IN ('BIOS_BUNDLE','PARENT')
ORDER BY role,sort_order,logical_name
`, variantID)
	if err != nil {
		return fmt.Errorf("load current variant bundle files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var role, logicalName, blobID string
		var sortOrder int
		if err := rows.Scan(&role, &logicalName, &blobID, &sortOrder); err != nil {
			return fmt.Errorf("scan current variant bundle file: %w", err)
		}
		virtualPath := fmt.Sprintf("/__retrom__/%s/%02d/%s", strings.ToLower(role), sortOrder, logicalName)
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_external_files(launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind)
VALUES(?,?,?,?,?,?)
`, launchID, virtualPath, logicalName, blobID, now, role); err != nil {
			return fmt.Errorf("lock current variant bundle file: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate current variant bundle files: %w", err)
	}
	return nil
}

func (service *Service) lockExternalBIOS(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, variantID string,
	now int64,
	allowMissing bool,
) error {
	var snapshotJSON, contentLogicalName string
	if err := transaction.QueryRowContext(ctx, `
SELECT variant.dependency_snapshot_json,content.logical_name
FROM game_variants variant
JOIN launch_content_files content ON content.launch_session_id=?
WHERE variant.id=? ORDER BY content.logical_name LIMIT 1
`, launchID, variantID).Scan(&snapshotJSON, &contentLogicalName); err != nil {
		return ErrBlocked
	}
	dependencies, err := corevalidation.ParseRuntimeBIOSDependencies(snapshotJSON)
	if err != nil {
		return ErrBlocked
	}
	seenLogicalNames, seenVirtualPaths, count, err := lockedExternalNames(ctx, transaction, launchID, contentLogicalName)
	if err != nil {
		return err
	}
	for _, dependency := range dependencies {
		if dependency.DeliveryKind != "EXTERNAL_FILE" {
			continue
		}
		if !availableExternalBIOS(dependency) {
			if allowMissing {
				continue
			}
			return ErrBlocked
		}
		count++
		if count > 16 {
			return ErrBlocked
		}
		logicalKey := strings.ToLower(dependency.LogicalName)
		if _, duplicate := seenLogicalNames[logicalKey]; duplicate {
			return ErrBlocked
		}
		if _, duplicate := seenVirtualPaths[*dependency.EmulatorPath]; duplicate {
			return ErrBlocked
		}
		seenLogicalNames[logicalKey] = struct{}{}
		seenVirtualPaths[*dependency.EmulatorPath] = struct{}{}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_external_files(launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind)
VALUES(?,?,?,?,?,'BIOS')
`, launchID, *dependency.EmulatorPath, dependency.LogicalName, *dependency.BlobID, now); err != nil {
			return fmt.Errorf("lock launch external BIOS: %w", err)
		}
	}
	return nil
}

func availableExternalBIOS(dependency corevalidation.BIOSDependency) bool {
	return dependency.EmulatorPath != nil && dependency.BlobID != nil && dependency.InstallationStatus != nil &&
		(*dependency.InstallationStatus == "MATCHED" || *dependency.InstallationStatus == "HASH_WARNING")
}

func lockedExternalNames(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, contentLogicalName string,
) (map[string]struct{}, map[string]struct{}, int, error) {
	seenLogicalNames := map[string]struct{}{strings.ToLower(path.Base(contentLogicalName)): {}}
	seenVirtualPaths := make(map[string]struct{})
	rows, err := transaction.QueryContext(ctx, `
SELECT virtual_path,logical_name FROM launch_external_files WHERE launch_session_id=? ORDER BY virtual_path
`, launchID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("lock launch external files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	count := 0
	for rows.Next() {
		var virtualPath, logicalName string
		if err := rows.Scan(&virtualPath, &logicalName); err != nil {
			return nil, nil, 0, fmt.Errorf("lock launch external files: %w", err)
		}
		logicalKey := strings.ToLower(logicalName)
		if _, duplicate := seenLogicalNames[logicalKey]; duplicate {
			return nil, nil, 0, ErrBlocked
		}
		if _, duplicate := seenVirtualPaths[virtualPath]; duplicate {
			return nil, nil, 0, ErrBlocked
		}
		seenLogicalNames[logicalKey] = struct{}{}
		seenVirtualPaths[virtualPath] = struct{}{}
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("lock launch external files: %w", err)
	}
	return seenLogicalNames, seenVirtualPaths, count, nil
}
