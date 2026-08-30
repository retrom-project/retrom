package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
)

// Contract branches stay contiguous for a single auditable decision.
func (service *Service) Create(ctx context.Context, profileID string, request CreateRequest) (Created, error) {
	if !validCreateRequest(profileID, request) {
		return Created{}, ErrBlocked
	}
	coreID := ""
	if request.CoreID != nil {
		coreID = *request.CoreID
	}
	selectionResult, err := service.selectLaunchVariant(ctx, profileID, request, coreID)
	if err != nil {
		return Created{}, err
	}
	if selectionResult.retry != nil {
		return *selectionResult.retry, nil
	}
	selection := selectionResult.selection
	preparation, err := service.prepareLaunch(ctx, request, selection)
	if err != nil {
		return Created{}, err
	}
	return service.persistLaunch(
		ctx, profileID, request, selection, preparation.contentPlan,
		preparation.selectedDOSEntry, preparation.initialDiscIndex,
	)
}

func validCreateRequest(profileID string, request CreateRequest) bool {
	return profileID != "" && request.GameID != "" &&
		validReturnTo(request.ReturnTo, request.GameID, request.SaveStateID)
}

type launchPreparation struct {
	contentPlan      launchContentPlan
	selectedDOSEntry *string
	initialDiscIndex int64
}

func (service *Service) prepareLaunch(
	ctx context.Context,
	request CreateRequest,
	selection launchSelection,
) (launchPreparation, error) {
	if !validThreadCapabilities(selection.requiresThreads, request.ClientCapabilities) {
		return launchPreparation{}, ErrBlocked
	}
	switch selection.runtimeFamily {
	case "RPGMAKER":
		return service.prepareRPGLaunch(ctx, selection)
	case "ONS":
		return service.prepareONSLaunch(ctx, selection)
	case "KIRIKIRI":
		return service.prepareKiriKiriLaunch(ctx, selection)
	case "BUTTERSCOTCH":
		return service.prepareButterscotchLaunch(ctx, selection)
	case "TYRANOSCRIPT":
		return service.prepareTyranoScriptLaunch(ctx, selection)
	case "EMULATORJS":
		return service.prepareEmulatorJSLaunch(ctx, request, selection)
	default:
		return launchPreparation{}, ErrBlocked
	}
}

func (service *Service) prepareRPGLaunch(
	ctx context.Context,
	selection launchSelection,
) (launchPreparation, error) {
	contentPlan, err := service.buildRPGProductContentPlan(ctx, selection)
	if err != nil {
		return launchPreparation{}, err
	}
	return launchPreparation{contentPlan: contentPlan}, nil
}

func (service *Service) prepareONSLaunch(
	ctx context.Context,
	selection launchSelection,
) (launchPreparation, error) {
	contentPlan, err := service.buildONSProductContentPlan(ctx, selection)
	if err != nil {
		return launchPreparation{}, err
	}
	return launchPreparation{contentPlan: contentPlan}, nil
}

func (service *Service) prepareKiriKiriLaunch(
	ctx context.Context,
	selection launchSelection,
) (launchPreparation, error) {
	contentPlan, err := service.buildKiriKiriProductContentPlan(ctx, selection)
	if err != nil {
		return launchPreparation{}, err
	}
	return launchPreparation{contentPlan: contentPlan}, nil
}

func (service *Service) prepareButterscotchLaunch(
	ctx context.Context,
	selection launchSelection,
) (launchPreparation, error) {
	contentPlan, err := service.buildButterscotchProductContentPlan(ctx, selection)
	if err != nil {
		return launchPreparation{}, err
	}
	return launchPreparation{contentPlan: contentPlan}, nil
}

func (service *Service) prepareTyranoScriptLaunch(
	ctx context.Context,
	selection launchSelection,
) (launchPreparation, error) {
	contentPlan, err := service.buildTyranoScriptProductContentPlan(ctx, selection)
	if err != nil {
		return launchPreparation{}, err
	}
	return launchPreparation{contentPlan: contentPlan}, nil
}

func (service *Service) prepareEmulatorJSLaunch(
	ctx context.Context,
	request CreateRequest,
	selection launchSelection,
) (launchPreparation, error) {
	if service.dependencies.Versions[selection.runtimeVersion] == nil {
		return launchPreparation{}, ErrBlocked
	}
	compatibility, err := service.loadArtifactCompatibility(ctx, selection.artifactID)
	if err != nil {
		return launchPreparation{}, ErrBlocked
	}
	selectedDOSEntry := request.DOSEntry
	if request.SaveStateID != nil && selection.savedDOSEntry.Valid {
		selectedDOSEntry = &selection.savedDOSEntry.String
	}
	if err := service.validateDOSEntry(ctx, selection.variantRevisionID, selectedDOSEntry); err != nil {
		return launchPreparation{}, err
	}
	contentPlan, err := service.buildLaunchContentPlan(
		ctx, selection.variantRevisionID, selection.selectedCore, compatibility,
	)
	if err != nil {
		return launchPreparation{}, err
	}
	if contentPlan.ContentKind != selection.contentKind {
		return launchPreparation{}, ErrBlocked
	}
	primary, ok := contentPlan.singleFile()
	if !ok {
		return launchPreparation{}, ErrBlocked
	}
	if err := service.validateLaunchLogicalNames(
		ctx, selection.variantRevisionID, primary.LogicalName,
	); err != nil {
		return launchPreparation{}, err
	}
	initialDiscIndex, err := launchInitialDiscIndex(
		selection.contentKind, request.SaveStateID != nil,
		selection.savedDiscIndex, len(contentPlan.Discs),
	)
	if err != nil {
		return launchPreparation{}, err
	}
	return launchPreparation{
		contentPlan: contentPlan, selectedDOSEntry: selectedDOSEntry, initialDiscIndex: initialDiscIndex,
	}, nil
}

func validThreadCapabilities(requiresThreads int, capabilities Capabilities) bool {
	return requiresThreads != 1 || capabilities.SecureContext &&
		capabilities.CrossOriginIsolated && capabilities.SharedArrayBuffer
}

type launchSelection struct {
	variantID, variantRevisionID, artifactID, selectedCore, runtimeVersion string
	contentRevisionID, contentLogicalName, contentKind, runtimeFamily      string
	routeKey, revisionCompatibilityCode                                    string
	revisionDATID                                                          sql.NullString
	requiresThreads                                                        int
	savedDOSEntry                                                          sql.NullString
	savedDiscIndex                                                         sql.NullInt64
}

type launchSelectionResult struct {
	selection launchSelection
	retry     *Created
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
	err := service.database.QueryRowContext(ctx, `
SELECT s.game_variant_revision_id,
a.id,
a.core_id,
a.runtime_version,
a.runtime_family,
a.requires_threads,
s.dos_entry_path,
s.disc_index,
s.game_content_revision_id,
content.content_kind,
r.route_key,
r.compatibility_code,
COALESCE((SELECT file.logical_name FROM game_content_files file
WHERE file.game_content_revision_id=r.game_content_revision_id
AND file.role IN ('CONTENT','DISC') ORDER BY CASE file.role WHEN 'CONTENT' THEN 0 ELSE 1 END,
file.sort_order,file.logical_name LIMIT 1),'')
FROM save_states s
JOIN games g ON g.id=s.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN game_variant_revisions r ON r.id=s.game_variant_revision_id
AND r.game_content_revision_id=s.game_content_revision_id
JOIN game_content_revisions content ON content.id=s.game_content_revision_id
JOIN core_artifacts writer ON writer.id=s.core_artifact_id
JOIN core_artifacts bound_artifact ON bound_artifact.id=r.core_artifact_id
JOIN core_artifacts a ON (
  writer.runtime_family='EMULATORJS' AND a.id=writer.id
  OR writer.runtime_family IN ('RPGMAKER','ONS','KIRIKIRI','BUTTERSCOTCH','TYRANOSCRIPT')
    AND a.core_id=writer.core_id AND a.route_key=writer.route_key
    AND a.runtime_family=writer.runtime_family
)
JOIN save_state_runtime_compatibility save_compatibility
  ON save_compatibility.save_state_id=s.id AND save_compatibility.status='AVAILABLE'
LEFT JOIN rpgmaker_variant_profiles rpg ON rpg.game_variant_revision_id=r.id
WHERE s.id=?
AND s.game_id=?
AND s.profile_id=?
AND s.deleted_at_ms IS NULL
AND g.status='PUBLISHED'
AND pi.enabled=1
AND r.status='READY'
AND a.available_for_launch=1
AND (a.runtime_family='EMULATORJS' OR a.selected_for_new_bindings=1)
AND (
  a.runtime_family='EMULATORJS' AND r.core_artifact_id=s.core_artifact_id
  OR a.runtime_family='ONS'
    AND bound_artifact.core_id=a.core_id AND bound_artifact.route_key=a.route_key
    AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
        json_extract(a.compatibility_json,'$.gameCompatibilityLine')
  OR a.runtime_family='KIRIKIRI'
    AND bound_artifact.core_id=a.core_id AND bound_artifact.route_key=a.route_key
    AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
        json_extract(a.compatibility_json,'$.gameCompatibilityLine')
  OR a.runtime_family='BUTTERSCOTCH'
    AND bound_artifact.core_id=a.core_id AND bound_artifact.route_key=a.route_key
    AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
        json_extract(a.compatibility_json,'$.gameCompatibilityLine')
  OR a.runtime_family='TYRANOSCRIPT'
    AND bound_artifact.core_id=a.core_id AND bound_artifact.route_key=a.route_key
    AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
        json_extract(a.compatibility_json,'$.gameCompatibilityLine')
  OR a.runtime_family='RPGMAKER'
    AND bound_artifact.core_id=a.core_id AND bound_artifact.route_key=a.route_key
    AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
        json_extract(a.compatibility_json,'$.gameCompatibilityLine')
    AND rpg.dependency_snapshot_sha256=s.dependency_snapshot_sha256
)
`, *request.SaveStateID, request.GameID, profileID).
		Scan(
			&selection.variantRevisionID, &selection.artifactID, &selection.selectedCore,
			&selection.runtimeVersion, &selection.runtimeFamily, &selection.requiresThreads, &selection.savedDOSEntry,
			&selection.savedDiscIndex, &selection.contentRevisionID, &selection.contentKind,
			&selection.routeKey, &selection.revisionCompatibilityCode, &selection.contentLogicalName,
		)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) && service.saveStateRuntimeIncompatible(
			ctx, profileID, request.GameID, *request.SaveStateID,
		) {
			return launchSelection{}, ErrSaveIncompatible
		}
		return launchSelection{}, ErrBlocked
	}
	if request.CoreID != nil && !requestedCoreMatchesSelection(coreID, selection) {
		return launchSelection{}, ErrBlocked
	}
	return selection, nil
}

func (service *Service) saveStateRuntimeIncompatible(
	ctx context.Context,
	profileID, gameID, saveStateID string,
) bool {
	var status string
	err := service.database.QueryRowContext(ctx, `
SELECT compatibility.status
FROM save_states save
JOIN games game ON game.id=save.game_id
JOIN save_state_runtime_compatibility compatibility ON compatibility.save_state_id=save.id
WHERE save.id=? AND save.profile_id=? AND save.game_id=? AND save.deleted_at_ms IS NULL
  AND game.status='PUBLISHED'
`, saveStateID, profileID, gameID).Scan(&status)
	return err == nil && status == "INCOMPATIBLE_RUNTIME"
}

func (service *Service) selectCurrentLaunchVariant(
	ctx context.Context,
	profileID string,
	request CreateRequest,
	coreID string,
) (launchSelectionResult, error) {
	var selection launchSelection
	var validationInputDigest string
	query := `
SELECT v.id,
v.current_revision_id,
a.id,
a.core_id,
a.runtime_version,
a.runtime_family,
a.requires_threads,
r.validation_input_digest,
r.compatibility_code,
r.game_content_revision_id,
r.dat_version_id,
content_revision.content_kind,
r.route_key,
COALESCE(content.logical_name,'')
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN game_variants v ON v.game_id=g.id
JOIN game_variant_revisions r ON r.id=v.current_revision_id
AND r.game_content_revision_id=g.current_content_revision_id
JOIN core_artifacts bound_artifact ON bound_artifact.id=r.core_artifact_id
JOIN core_artifacts a ON (
  bound_artifact.runtime_family='EMULATORJS' AND a.id=bound_artifact.id
  OR bound_artifact.runtime_family IN ('RPGMAKER','ONS','KIRIKIRI','BUTTERSCOTCH','TYRANOSCRIPT')
    AND a.core_id=bound_artifact.core_id AND a.route_key=r.route_key
    AND a.runtime_family=bound_artifact.runtime_family
    AND json_extract(a.compatibility_json,'$.gameCompatibilityLine')=
        json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')
)
AND a.available_for_launch=1 AND a.selected_for_new_bindings=1
JOIN game_content_revisions content_revision ON content_revision.id=r.game_content_revision_id
LEFT JOIN game_content_files content ON content.game_content_revision_id=r.game_content_revision_id
AND content.role IN ('CONTENT','DISC')
WHERE g.id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
AND r.status='READY'
AND (a.runtime_family IN ('EMULATORJS','ONS','KIRIKIRI','BUTTERSCOTCH','TYRANOSCRIPT') OR EXISTS(
  SELECT 1 FROM rpgmaker_variant_profiles profile
  WHERE profile.game_variant_revision_id=r.id AND profile.route_key=r.route_key
))
AND v.core_id=CASE
 WHEN ?='' OR (?='rpgmaker' AND pi.platform_id='rpgmaker')
 THEN CASE WHEN pi.platform_id='rpgmaker' THEN v.core_id ELSE pi.default_core_id END
 ELSE ? END
ORDER BY CASE content.role WHEN 'CONTENT' THEN 0 ELSE 1 END,content.sort_order,content.logical_name
LIMIT 1
`
	if err := service.database.QueryRowContext(ctx, query, request.GameID, coreID, coreID, coreID).Scan(
		&selection.variantID,
		&selection.variantRevisionID,
		&selection.artifactID,
		&selection.selectedCore,
		&selection.runtimeVersion,
		&selection.runtimeFamily,
		&selection.requiresThreads,
		&validationInputDigest,
		&selection.revisionCompatibilityCode,
		&selection.contentRevisionID,
		&selection.revisionDATID,
		&selection.contentKind,
		&selection.routeKey,
		&selection.contentLogicalName,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if service.isRPGMakerCore(ctx, coreID, request.GameID) {
				return launchSelectionResult{}, ErrBlocked
			}
			created, ensureErr := service.ensureVariant(ctx, profileID, request, coreID, true)
			return launchSelectionResult{retry: &created}, ensureErr
		}
		return launchSelectionResult{}, ErrBlocked
	}
	if selection.revisionCompatibilityCode == reviewScreenshotOverrideCode {
		return launchSelectionResult{selection: selection}, nil
	}
	if selection.runtimeFamily == "RPGMAKER" || selection.runtimeFamily == "ONS" ||
		selection.runtimeFamily == "KIRIKIRI" || selection.runtimeFamily == "BUTTERSCOTCH" ||
		selection.runtimeFamily == "TYRANOSCRIPT" {
		return launchSelectionResult{selection: selection}, nil
	}
	expectedDigest, err := service.currentVariantDigest(ctx, selection)
	if err != nil {
		return launchSelectionResult{}, ErrBlocked
	}
	if validationInputDigest != expectedDigest {
		created, ensureErr := service.ensureVariant(ctx, profileID, request, coreID, true)
		return launchSelectionResult{retry: &created}, ensureErr
	}
	return launchSelectionResult{selection: selection}, nil
}

func requestedCoreMatchesSelection(coreID string, selection launchSelection) bool {
	return coreID == selection.selectedCore || coreID == "rpgmaker" && selection.runtimeFamily == "RPGMAKER"
}

func (service *Service) currentVariantDigest(ctx context.Context, selection launchSelection) (string, error) {
	biosSnapshot, biosStatus, _, err := service.resolveVariantBIOS(
		ctx, service.database, selection.variantID, selection.contentRevisionID,
		selection.artifactID, selection.contentLogicalName, selection.revisionDATID,
	)
	if err != nil || biosStatus != "READY" {
		return "", ErrBlocked
	}
	if selection.contentKind == corevalidation.MultiDiscContentKind {
		return service.expectedMultiDiscDigest(
			ctx, selection.variantRevisionID, selection.contentRevisionID,
			selection.artifactID, selection.revisionDATID, biosSnapshot,
		)
	}
	digest, err := corevalidation.ValidationInputDigest(
		selection.artifactID, selection.contentRevisionID, selection.revisionDATID, biosSnapshot,
	)
	if err != nil {
		return "", fmt.Errorf("digest current launch variant: %w", err)
	}
	return digest, nil
}

func (service *Service) validateDOSEntry(
	ctx context.Context,
	variantRevisionID string,
	selectedDOSEntry *string,
) error {
	if selectedDOSEntry == nil {
		return nil
	}
	var directLaunchSafe int
	err := service.database.QueryRowContext(ctx, `
SELECT d.direct_launch_safe
FROM game_variant_revisions r
JOIN dos_entries d ON d.game_content_revision_id=r.game_content_revision_id
WHERE r.id=?
AND d.normalized_path=?
AND d.enabled=1
`, variantRevisionID, *selectedDOSEntry).
		Scan(&directLaunchSafe)
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
	contentPlan launchContentPlan,
	selectedDOSEntry *string,
	initialDiscIndex int64,
) (Created, error) {
	variantRevisionID, artifactID := selection.variantRevisionID, selection.artifactID
	revisionCompatibilityCode := selection.revisionCompatibilityCode
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
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO launch_sessions(id,profile_id,purpose,game_id,game_content_revision_id,
game_variant_revision_id,core_artifact_id,route_key,save_state_id,dos_entry_path,
initial_disc_index,return_to,credential_sha256,state,bootstrap_expires_at_ms,
hard_expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,'PRODUCT',?,?,?,?,?,?,?,?,?,?,'CREATED',?,?,?,?)
`,
		launchID.String(),
		profileID,
		request.GameID,
		selection.contentRevisionID,
		variantRevisionID,
		artifactID,
		selection.routeKey,
		request.SaveStateID,
		selectedDOSEntry,
		initialDiscIndex,
		request.ReturnTo,
		capabilityHash[:],
		bootstrapExpires,
		hardExpires,
		now,
		now,
	)
	if err != nil {
		return Created{}, fmt.Errorf("create launch session: %w", err)
	}
	if selection.runtimeFamily == "RPGMAKER" || selection.runtimeFamily == "TYRANOSCRIPT" {
		if err := service.lockNativeBootstrapTicket(
			ctx, transaction, launchID.String(), profileID, artifactID, now,
		); err != nil {
			return Created{}, err
		}
	}
	for _, file := range contentPlan.Files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,
logical_name,
blob_id,
format_version,
created_at_ms) VALUES(?,
?,
?,
?,
?)
	`, launchID.String(), file.LogicalName, file.BlobID, file.Format, now); err != nil {
			return Created{}, fmt.Errorf("lock launch content: %w", err)
		}
	}
	for _, disc := range contentPlan.Discs {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_external_files(launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind)
VALUES(?,?,?,?,?,'DISC')
`, launchID.String(), disc.VirtualPath, disc.LogicalName, disc.BlobID, now); err != nil {
			return Created{}, fmt.Errorf("lock launch disc: %w", err)
		}
	}
	if selection.runtimeFamily == "EMULATORJS" {
		if err := service.lockExternalBIOS(
			ctx, transaction, launchID.String(), variantRevisionID, now,
			revisionCompatibilityCode == reviewScreenshotOverrideCode,
		); err != nil {
			return Created{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("commit launch session: %w", err)
	}
	return Created{
		LaunchID:             launchID.String(),
		PlayURL:              "/play/" + launchID.String(),
		Warnings:             []string{},
		BootstrapExpiresAtMS: bootstrapExpires,
		HardExpiresAtMS:      hardExpires,
		Capability:           retromruntime.EncodeCapability(capability),
	}, nil
}

func (service *Service) loadArtifactCompatibility(
	ctx context.Context,
	artifactID string,
) (artifactCompatibility, error) {
	var raw string
	if err := service.database.QueryRowContext(ctx, `
SELECT compatibility_json
FROM core_artifacts
WHERE id=? AND runtime_family='EMULATORJS' AND available_for_launch=1
`, artifactID).Scan(&raw); err != nil {
		return artifactCompatibility{}, fmt.Errorf("launch/artifact compatibility: %w", err)
	}
	var compatibility artifactCompatibility
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compatibility); err != nil || !validArtifactCompatibility(compatibility) {
		return artifactCompatibility{}, ErrBlocked
	}
	return compatibility, nil
}

func validArtifactCompatibility(compatibility artifactCompatibility) bool {
	if !validArtifactCompatibilitySchema(compatibility) || compatibility.DefaultOptions == nil ||
		compatibility.StartupActions == nil {
		return false
	}
	if len(compatibility.DefaultOptions) > 32 || len(compatibility.StartupActions) > 4 {
		return false
	}
	if !validRuntimeCoreID(compatibility.RuntimeCoreID) ||
		!validRequestedArtifactBasename(compatibility.RequestedArtifactBasename) {
		return false
	}
	if compatibility.CanvasResizePolicy != "NONE" &&
		compatibility.CanvasResizePolicy != "ON_GAME_START_TO_CSS_PIXELS" {
		return false
	}
	if compatibility.InputMode != "STANDARD" && compatibility.InputMode != "POINTER" {
		return false
	}
	return validDefaultOptions(compatibility.DefaultOptions) &&
		validStartupActions(compatibility.StartupActions)
}

func validArtifactCompatibilitySchema(compatibility artifactCompatibility) bool {
	return compatibility.SchemaVersion == 5 && validContentCapabilities(compatibility)
}

func validContentCapabilities(compatibility artifactCompatibility) bool {
	if len(compatibility.SupportedContentKinds) != 1 && len(compatibility.SupportedContentKinds) != 2 {
		return false
	}
	for index, kind := range compatibility.SupportedContentKinds {
		if kind != "SINGLE_FILE" && kind != "DOS_BUNDLE" && kind != "MULTI_DISC_M3U_V1" ||
			index > 0 && kind == compatibility.SupportedContentKinds[index-1] {
			return false
		}
	}
	if compatibility.MultiDisc == nil {
		return !slices.Contains(compatibility.SupportedContentKinds, "MULTI_DISC_M3U_V1")
	}
	return slices.Contains(compatibility.SupportedContentKinds, "MULTI_DISC_M3U_V1") &&
		compatibility.MultiDisc.MaxDiscs >= 2 && compatibility.MultiDisc.MaxDiscs <= 8 &&
		compatibility.MultiDisc.MaxTotalBytes >= 1 && compatibility.MultiDisc.MaxTotalBytes <= 1_073_741_824 &&
		compatibility.MultiDisc.Delivery == "EAGER_EXTERNAL_FILES"
}

func validRuntimeCoreID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character != '_' && (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validRequestedArtifactBasename(value string) bool {
	return path.Base(value) == value && !strings.Contains(value, `\`) &&
		!strings.Contains(value, "..") && strings.HasSuffix(value, "-wasm.data")
}

func validDefaultOptions(options map[string]string) bool {
	for name, value := range options {
		if name == "__proto__" || name == "prototype" || name == "constructor" ||
			!printableASCII(name, 1, 128) || !printableASCII(value, 0, 128) {
			return false
		}
	}
	return true
}

func validStartupActions(actions []dependencies.StartupAction) bool {
	for _, action := range actions {
		if action.Event != "GAME_START" || action.Kind != "PRESS_CONTROL" ||
			action.DelayMS < 0 || action.DelayMS > 30_000 || action.Player < 0 || action.Player > 3 ||
			action.Control < 0 || action.Control > 255 || action.DurationMS < 1 || action.DurationMS > 1_000 {
			return false
		}
	}
	return true
}

func printableASCII(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func (service *Service) lockExternalBIOS(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, variantRevisionID string,
	now int64,
	allowMissing bool,
) error {
	var snapshotJSON, contentLogicalName string
	if err := transaction.QueryRowContext(ctx, `
SELECT revision.dependency_snapshot_json,content.logical_name
FROM game_variant_revisions revision
JOIN launch_content_files content ON content.launch_session_id=?
WHERE revision.id=?
`, launchID, variantRevisionID).Scan(&snapshotJSON, &contentLogicalName); err != nil {
		return ErrBlocked
	}
	dependencies, err := corevalidation.ParseRuntimeBIOSDependencies(snapshotJSON)
	if err != nil {
		return ErrBlocked
	}
	seenLogicalNames, seenVirtualPaths, count, err := lockedExternalNames(
		ctx, transaction, launchID, contentLogicalName,
	)
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
INSERT INTO launch_external_files(launch_session_id,
virtual_path,
logical_name,
blob_id,
created_at_ms,
kind) VALUES(?,?,?,?,?,'BIOS')
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
	seenLogicalNames := map[string]struct{}{strings.ToLower(contentLogicalName): {}}
	seenVirtualPaths := make(map[string]struct{})
	existingRows, err := transaction.QueryContext(ctx, `
SELECT virtual_path,logical_name
FROM launch_external_files
WHERE launch_session_id=?
ORDER BY virtual_path
`, launchID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("lock launch external files: %w", err)
	}
	defer func() { cleanup.Error("close", existingRows.Close()) }()
	count := 0
	for existingRows.Next() {
		var virtualPath, logicalName string
		if err := existingRows.Scan(&virtualPath, &logicalName); err != nil {
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
	if err := existingRows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("lock launch external files: %w", err)
	}
	return seenLogicalNames, seenVirtualPaths, count, nil
}

func (service *Service) validateLaunchLogicalNames(
	ctx context.Context,
	variantRevisionID, contentLogicalName string,
) error {
	seen := map[string]struct{}{strings.ToLower(contentLogicalName): {}}
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT logical_name
FROM variant_files
WHERE game_variant_revision_id=?
AND role IN ('PARENT',
'BIOS_BUNDLE')
ORDER BY role,
sort_order,
logical_name
`,
		variantRevisionID,
	)
	if err != nil {
		return fmt.Errorf("launch/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var logicalName string
		if err := rows.Scan(&logicalName); err != nil {
			return fmt.Errorf("launch/service: %w", err)
		}
		key := strings.ToLower(logicalName)
		if _, exists := seen[key]; exists || logicalName == "" || path.Base(logicalName) != logicalName ||
			strings.Contains(logicalName, `\`) {
			return ErrBlocked
		}
		seen[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan launch content files: %w", err)
	}
	return nil
}

func (service *Service) lockLaunchContent(
	ctx context.Context,
	variantRevisionID, coreID string,
) (string, string, string, error) {
	if coreID != "dosbox_pure" {
		var blobID, logicalName string
		err := service.database.QueryRowContext(ctx, `
SELECT f.blob_id,
f.logical_name
FROM game_variant_revisions r
JOIN game_content_files f ON f.game_content_revision_id=r.game_content_revision_id
AND f.role='CONTENT'
WHERE r.id=?
ORDER BY f.sort_order,
f.logical_name LIMIT 1
`, variantRevisionID).
			Scan(&blobID, &logicalName)
		if err != nil {
			return "", "", "", ErrBlocked
		}
		return blobID, logicalName, "SOURCE_V1", nil
	}
	var baseBlobID string
	err := service.database.QueryRowContext(ctx, `
SELECT vf.blob_id
FROM variant_files vf
WHERE vf.game_variant_revision_id=?
AND vf.role='DOS_LAUNCH_BUNDLE'
AND vf.logical_name='game.zip'
`, variantRevisionID).
		Scan(&baseBlobID)
	if err != nil {
		return "", "", "", ErrBlocked
	}
	return baseBlobID, "game.zip", "RETROM_DOS_DIRECT_ZIP_V1", nil
}

func validReturnTo(value, gameID string, saveStateID *string) bool {
	if strings.ContainsAny(value, "#%\\") {
		return false
	}
	if value == "/" || value == "/library" || value == "/saves" || value == "/games/"+gameID {
		return true
	}
	return validImmersiveReturnTo(value, gameID, saveStateID)
}

func validImmersiveReturnTo(value, gameID string, saveStateID *string) bool {
	if strings.HasPrefix(value, "/immersive/platforms/") {
		return saveStateID == nil && validImmersivePlatformReturn(value, gameID)
	}
	return validImmersiveLibraryReturn(value, gameID, saveStateID)
}

func validImmersivePlatformReturn(value, gameID string) bool {
	const prefix = "/immersive/platforms/"
	const separator = "?gameId="
	if !strings.HasPrefix(value, prefix) || strings.Count(value, "?") != 1 {
		return false
	}
	platformID, returnedGameID, found := strings.Cut(strings.TrimPrefix(value, prefix), separator)
	if !found || returnedGameID != gameID || len(platformID) == 0 || len(platformID) > 64 {
		return false
	}
	for _, character := range platformID {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func validImmersiveLibraryReturn(value, gameID string, saveStateID *string) bool {
	pathAndQuery := strings.Split(value, "?")
	if len(pathAndQuery) != 2 {
		return false
	}
	query, valid := parseImmersiveReturnQuery(pathAndQuery[1])
	if !valid || query["gameId"] != gameID {
		return false
	}
	switch pathAndQuery[0] {
	case "/immersive/library/all", "/immersive/library/recent":
		return saveStateID == nil && len(query) == 1
	case "/immersive/library/favorites":
		return saveStateID == nil && (len(query) == 1 ||
			len(query) == 2 && validCanonicalUUID(query["folderId"]))
	case "/immersive/library/saves":
		return saveStateID != nil && len(query) == 2 && query["saveStateId"] == *saveStateID
	default:
		return false
	}
}

func parseImmersiveReturnQuery(value string) (map[string]string, bool) {
	result := make(map[string]string)
	for _, entry := range strings.Split(value, "&") {
		name, entryValue, found := strings.Cut(entry, "=")
		if !found || name == "" || entryValue == "" {
			return nil, false
		}
		if _, duplicate := result[name]; duplicate {
			return nil, false
		}
		result[name] = entryValue
	}
	return result, true
}

func validCanonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}
