package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"retrom/internal/cleanup"
	rpgvalidation "retrom/internal/rpgmaker/validation"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/runtimebundle"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimelaunch"
	"retrom/internal/runtimeoptions"
)

// Config is an already closed-validated Launch Envelope. Keeping the encoded
// value opaque prevents HTTP and callers from growing a second runtime DTO.
type Config struct{ contents json.RawMessage }

func (configuration Config) MarshalJSON() ([]byte, error) {
	if len(configuration.contents) == 0 {
		return nil, runtimelaunch.ErrEnvelopeInvalid
	}
	return slices.Clone(configuration.contents), nil
}

type MultiDiscTelemetryDimensions struct {
	PlatformKey  string
	TargetKey    string
	BundleDigest string
	DiscCount    int
}

type BundleFile struct {
	LogicalName string
	SHA256      string
}

type DiscSet struct {
	ContentKind      string
	Count            int
	InitialDiscIndex int
	Entries          []DiscEntry
}

type DiscEntry struct {
	Index       int
	Label       string
	VirtualPath string
}

type providerConfigSource struct {
	credentialHash  []byte
	state           string
	providerID      string
	targetID        string
	bundleDigest    string
	coreID          string
	coreName        string
	detectorProfile string
	delivery        string
	purpose         string
	title           string
	platformName    string
	returnTo        string
	contentKind     string
	generation      string
	dependencyJSON  string
	compatibility   string
	saveID          sql.NullString
	dosEntry        sql.NullString
	validationID    sql.NullString
	netplayID       sql.NullString
	netplayPlayer   sql.NullInt64
	netplayRoom     sql.NullString
	netplayProfile  sql.NullString
	bootstrapEnd    int64
	hardEnd         int64
	idleEnd         sql.NullInt64
	initialDisc     int64
}

func (service *Service) Config(ctx context.Context, launchID, capability string) (Config, error) {
	source, err := service.productConfigSource(ctx, launchID)
	if err != nil {
		source, err = service.validationConfigSource(ctx, launchID)
	}
	if err != nil || service.runtimeBuilder == nil ||
		!retromruntime.MatchesCapability(capability, source.credentialHash) ||
		!validConfigLifetime(source.state, source.bootstrapEnd, source.hardEnd, source.idleEnd, service.now().UnixMilli()) {
		return Config{}, ErrCredential
	}
	if err := service.activateLaunch(ctx, launchID, source.state); err != nil {
		return Config{}, err
	}
	return service.providerEnvelope(ctx, launchID, capability, source)
}

func (service *Service) productConfigSource(ctx context.Context, launchID string) (providerConfigSource, error) {
	var source providerConfigSource
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.provider_id,launch.target_id,
 launch.bundle_sha256,
 launch.core_id,core.name,binding.detector_profile,binding.delivery_profile,
 launch.purpose,game.title,platform.name,launch.return_to,
 launch.content_kind,launch.dependency_snapshot_json,launch.compatibility_code,
 launch.save_state_id,launch.dos_entry_path,launch.rpgmaker_runtime_validation_id,
 launch.netplay_session_id,launch.netplay_player_no,session.room_id,session.profile_json,
 launch.bootstrap_expires_at_ms,launch.hard_expires_at_ms,launch.idle_expires_at_ms,
 launch.initial_disc_index
FROM launch_sessions launch
JOIN cores core ON core.id=launch.core_id
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN runtime_target_bindings binding ON binding.provider_id=launch.provider_id AND binding.target_id=launch.target_id
LEFT JOIN netplay_sessions session ON session.id=launch.netplay_session_id
WHERE launch.id=? AND launch.purpose='PRODUCT'
`, launchID).Scan(
		&source.credentialHash, &source.state, &source.providerID, &source.targetID,
		&source.bundleDigest, &source.coreID, &source.coreName,
		&source.detectorProfile, &source.delivery, &source.purpose, &source.title, &source.platformName, &source.returnTo,
		&source.contentKind, &source.dependencyJSON, &source.compatibility, &source.saveID,
		&source.dosEntry, &source.validationID, &source.netplayID, &source.netplayPlayer,
		&source.netplayRoom, &source.netplayProfile, &source.bootstrapEnd, &source.hardEnd,
		&source.idleEnd, &source.initialDisc,
	)
	if err != nil {
		return providerConfigSource{}, ErrCredential
	}
	return source, nil
}

func (service *Service) validationConfigSource(ctx context.Context, launchID string) (providerConfigSource, error) {
	var source providerConfigSource
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.provider_id,launch.target_id,
 launch.bundle_sha256,
 launch.core_id,core.name,binding.detector_profile,binding.delivery_profile,
 launch.purpose,'RPG Maker runtime validation',instance.name,
 launch.return_to,launch.content_kind,launch.dependency_snapshot_json,launch.compatibility_code,
 NULL,NULL,launch.rpgmaker_runtime_validation_id,
 NULL,NULL,NULL,NULL,launch.bootstrap_expires_at_ms,launch.hard_expires_at_ms,
 launch.idle_expires_at_ms,launch.initial_disc_index,validation.generation
FROM launch_sessions launch
JOIN rpgmaker_runtime_validations validation ON validation.id=launch.rpgmaker_runtime_validation_id
JOIN import_item_source_snapshots snapshot ON snapshot.id=validation.effective_source_snapshot_id
JOIN review_drafts draft ON draft.import_item_id=validation.import_item_id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
JOIN runtime_target_bindings binding ON binding.provider_id=launch.provider_id AND binding.target_id=launch.target_id
JOIN cores core ON core.id=launch.core_id
WHERE launch.id=? AND launch.purpose='RPG_RUNTIME_VALIDATION'
 AND validation.provider_id=launch.provider_id AND validation.target_id=launch.target_id
`, launchID).Scan(
		&source.credentialHash, &source.state, &source.providerID, &source.targetID,
		&source.bundleDigest, &source.coreID, &source.coreName,
		&source.detectorProfile, &source.delivery, &source.purpose, &source.title, &source.platformName, &source.returnTo,
		&source.contentKind, &source.dependencyJSON, &source.compatibility, &source.saveID,
		&source.dosEntry, &source.validationID, &source.netplayID, &source.netplayPlayer,
		&source.netplayRoom, &source.netplayProfile, &source.bootstrapEnd, &source.hardEnd,
		&source.idleEnd, &source.initialDisc, &source.generation,
	)
	if err != nil {
		return providerConfigSource{}, ErrCredential
	}
	return source, nil
}

func (service *Service) providerEnvelope(
	ctx context.Context,
	sessionID, capability string,
	source providerConfigSource,
) (Config, error) {
	target, exists := service.runtimeBuilder.Target(source.providerID, source.targetID)
	bundleDigest, bundleExists := service.runtimeBuilder.BundleSHA256(source.providerID, source.targetID)
	if !exists || !bundleExists || bundleDigest != source.bundleDigest {
		return Config{}, ErrCredential
	}
	resources, err := service.providerResources(ctx, sessionID, capability, source, target)
	if err != nil {
		return Config{}, err
	}
	restore, _, err := service.providerRestore(ctx, sessionID, source, target)
	if err != nil {
		return Config{}, err
	}
	netplay, mode, err := service.providerNetplay(source)
	if err != nil {
		return Config{}, err
	}
	purpose := source.purpose
	if purpose == "RPG_RUNTIME_VALIDATION" {
		purpose = "RUNTIME_VALIDATION"
	}
	validation := any(nil)
	var expectedRestorePosition *rpgvalidation.Position
	if purpose == "RUNTIME_VALIDATION" {
		resume, resumeErr := service.providerValidationResume(ctx, sessionID, source)
		if resumeErr != nil {
			return Config{}, resumeErr
		}
		position, restorePositionPresent, resumeErr := providerExpectedRestorePosition(sessionID, resume)
		if resumeErr != nil {
			return Config{}, resumeErr
		}
		if restorePositionPresent {
			expectedRestorePosition = &position
		}
		validation = map[string]any{"probeId": "rpgmaker.position.v1", "input": map[string]any{
			"generation": source.generation, "resume": resume,
		}}
	}
	options, err := providerTargetOptions(target.TargetOptionsSchema, source, expectedRestorePosition)
	if err != nil {
		return Config{}, err
	}
	contents, err := service.runtimeBuilder.Build(runtimelaunch.Input{
		Binding: runtimecatalog.Binding{
			ProviderID: source.providerID, TargetID: source.targetID,
			CoreID: source.coreID, LaunchPolicy: "SUPPORTED",
		},
		Session: runtimelaunch.Session{
			ID: sessionID, Purpose: purpose, Mode: mode, Title: source.title,
			PlatformName: source.platformName, CoreName: source.coreName,
			ReturnTo: source.returnTo, Warnings: providerWarnings(source),
		},
		Resources: resources, TargetOptions: options, Restore: restore,
		Validation: validation, Netplay: netplay,
	})
	if err != nil {
		return Config{}, fmt.Errorf("build Provider launch envelope: %w", err)
	}
	return Config{contents: contents}, nil
}

type providerValidationCheckpoint struct {
	CheckpointFormat string `json:"checkpointFormat"`
	SizeBytes        int64  `json:"sizeBytes"`
	SHA256           string `json:"sha256"`
}

type providerValidationResume struct {
	ValidationID              string                        `json:"validationId"`
	State                     string                        `json:"state"`
	OriginalLaunchID          string                        `json:"originalLaunchId"`
	RestoreLaunchID           *string                       `json:"restoreLaunchId"`
	LastGateSequence          int64                         `json:"lastGateSequence"`
	MachineGates              []json.RawMessage             `json:"machineGates"`
	CheckpointEvidence        *providerValidationCheckpoint `json:"checkpointEvidence"`
	RestoreScreenshotUploaded bool                          `json:"restoreScreenshotUploaded"`
}

func (service *Service) providerValidationResume(
	ctx context.Context,
	launchID string,
	source providerConfigSource,
) (providerValidationResume, error) {
	if !source.validationID.Valid {
		return providerValidationResume{}, ErrCredential
	}
	var resume providerValidationResume
	var restoreID, screenshotID, format, digest sql.NullString
	var size sql.NullInt64
	var machineJSON string
	err := service.database.QueryRowContext(ctx, `
SELECT validation.id,validation.state,validation.launch_id,validation.restore_launch_id,
 validation.last_gate_sequence,validation.machine_gates_json,validation.evidence_screenshot_blob_id,
 checkpoint.checkpoint_format,checkpoint.size_bytes,checkpoint.payload_sha256
FROM rpgmaker_runtime_validations validation
LEFT JOIN rpgmaker_runtime_validation_checkpoints checkpoint ON checkpoint.validation_id=validation.id
WHERE validation.id=? AND (validation.launch_id=? OR validation.restore_launch_id=?)
`, source.validationID.String, launchID, launchID).Scan(
		&resume.ValidationID, &resume.State, &resume.OriginalLaunchID, &restoreID,
		&resume.LastGateSequence, &machineJSON, &screenshotID, &format, &size, &digest,
	)
	if err != nil || resume.OriginalLaunchID == "" || json.Unmarshal([]byte(machineJSON), &resume.MachineGates) != nil ||
		len(resume.MachineGates) != 14 {
		return providerValidationResume{}, ErrCredential
	}
	if restoreID.Valid {
		resume.RestoreLaunchID = &restoreID.String
	}
	resume.RestoreScreenshotUploaded = screenshotID.Valid
	if format.Valid && size.Valid && digest.Valid {
		resume.CheckpointEvidence = &providerValidationCheckpoint{
			CheckpointFormat: format.String, SizeBytes: size.Int64, SHA256: digest.String,
		}
	} else if format.Valid || size.Valid || digest.Valid {
		return providerValidationResume{}, ErrCredential
	}
	return resume, nil
}

func providerWarnings(source providerConfigSource) []string {
	warnings := make([]string, 0, 2)
	if strings.Contains(source.dependencyJSON, `"installationStatus":"HASH_WARNING"`) {
		warnings = append(warnings, "BIOS_HASH_WARNING")
	}
	if source.compatibility == "REVIEW_SCREENSHOT_OVERRIDE" {
		warnings = append(warnings, "REVIEW_SCREENSHOT_OVERRIDE")
	}
	return warnings
}

func providerExpectedRestorePosition(
	launchID string,
	resume providerValidationResume,
) (rpgvalidation.Position, bool, error) {
	if resume.RestoreLaunchID == nil || *resume.RestoreLaunchID != launchID {
		return rpgvalidation.Position{}, false, nil
	}
	for _, encoded := range resume.MachineGates {
		evidence, recorded := providerRecordedPositionEvidence(encoded)
		if !recorded {
			continue
		}
		position, err := providerPositionEvidence(evidence)
		if err != nil {
			return rpgvalidation.Position{}, false, err
		}
		return position, true, nil
	}
	return rpgvalidation.Position{}, false, ErrCredential
}

func providerRecordedPositionEvidence(encoded json.RawMessage) (json.RawMessage, bool) {
	var gate struct {
		Gate     string          `json:"gate"`
		Status   string          `json:"status"`
		Evidence json.RawMessage `json:"evidence"`
	}
	if json.Unmarshal(encoded, &gate) != nil || gate.Gate != "SAVE_POINT_RECORDED" || gate.Status != "PASSED" {
		return nil, false
	}
	return gate.Evidence, true
}

func providerPositionEvidence(evidence json.RawMessage) (rpgvalidation.Position, error) {
	var shape map[string]json.RawMessage
	var position rpgvalidation.Position
	if json.Unmarshal(evidence, &shape) != nil || len(shape) != 4 ||
		shape["mapId"] == nil || shape["playerX"] == nil || shape["playerY"] == nil ||
		shape["fixtureState"] == nil || json.Unmarshal(evidence, &position) != nil ||
		position.MapID < 0 || position.PlayerX < 0 || position.PlayerY < 0 || position.FixtureState < 0 {
		return rpgvalidation.Position{}, ErrCredential
	}
	return position, nil
}

func providerTargetOptions(
	schema runtimebundle.TargetOptionsSchema,
	source providerConfigSource,
	expectedRestorePosition *rpgvalidation.Position,
) (map[string]any, error) {
	selected, registered := runtimecatalog.Strategy(source.detectorProfile)
	if !registered {
		return nil, runtimeoptions.ErrUnsupported
	}
	var dos *string
	if source.dosEntry.Valid {
		dos = &source.dosEntry.String
	}
	options, err := runtimeoptions.Build(selected.Options, schema, runtimeoptions.Input{
		DOSEntry: dos, ContentKind: source.contentKind, InitialDiscIndex: source.initialDisc,
		DependencySnapshot: source.dependencyJSON, ExpectedRestorePosition: expectedRestorePosition,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble Host launch options: %w", err)
	}
	return options, nil
}

type lockedProviderFile struct {
	logicalName string
	format      string
	digest      string
	size        int64
}

func (service *Service) providerResources(
	ctx context.Context,
	sessionID, capability string,
	source providerConfigSource,
	target runtimebundle.Target,
) ([]map[string]any, error) {
	files, err := service.lockedProviderFiles(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	resources := make([]map[string]any, 0, len(target.Inputs))
	for _, input := range target.Inputs {
		values, inputErr := service.providerInputResources(
			ctx, sessionID, capability, source, input, files,
		)
		if inputErr != nil {
			return nil, inputErr
		}
		for ordinal, value := range values {
			value["role"], value["ordinal"] = input.Role, ordinal
			resources = append(resources, value)
		}
	}
	return resources, nil
}

func (service *Service) lockedProviderFiles(ctx context.Context, launchID string) ([]lockedProviderFile, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT file.logical_name,file.format_version,blob.sha256,blob.size_bytes
FROM launch_content_files file JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=?
UNION ALL
SELECT preview.content_logical_name,preview.content_format,blob.sha256,blob.size_bytes
FROM review_preview_sessions preview JOIN blobs blob ON blob.id=preview.content_blob_id
WHERE preview.id=?
UNION ALL
SELECT file.logical_name,preview.content_format,blob.sha256,blob.size_bytes
FROM review_preview_files file
JOIN review_preview_sessions preview ON preview.id=file.preview_session_id
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.preview_session_id=? AND file.role='PROJECT_FILE'
ORDER BY 1
`, launchID, launchID, launchID)
	if err != nil {
		return nil, fmt.Errorf("load locked Provider files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]lockedProviderFile, 0)
	for rows.Next() {
		var file lockedProviderFile
		if err := rows.Scan(&file.logicalName, &file.format, &file.digest, &file.size); err != nil {
			return nil, fmt.Errorf("scan locked Provider files: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked Provider files: %w", err)
	}
	return files, nil
}

func (service *Service) providerGameResource(
	ctx context.Context,
	sessionID, capability string,
	source providerConfigSource,
	kind string,
	files []lockedProviderFile,
) (map[string]any, error) {
	switch kind {
	case "FILE_TREE":
		return service.providerFileTreeResource(ctx, sessionID, capability, kind)
	case "SEEKABLE_BLOB":
		identity, err := service.ProjectContentIdentity(ctx, sessionID, capability)
		if err != nil {
			return nil, err
		}
		return providerSeekableProjectResource(identity, files)
	case "NATIVE_WEB", "ISOLATED_WEB":
		return service.providerWebResource(ctx, sessionID, capability, source, kind)
	}
	return providerBlobResource(source, kind, files)
}

func (service *Service) providerFileTreeResource(
	ctx context.Context,
	sessionID, capability, kind string,
) (map[string]any, error) {
	identity, err := service.ProjectContentIdentity(ctx, sessionID, capability)
	if err != nil {
		return nil, err
	}
	root, err := RuntimeProjectContentRoot(identity)
	if err != nil {
		return nil, err
	}
	return map[string]any{"kind": kind, "indexUrl": root + "index.json", "contentDigest": identity}, nil
}

func (service *Service) providerWebResource(
	ctx context.Context,
	sessionID, capability string,
	source providerConfigSource,
	kind string,
) (map[string]any, error) {
	identity, err := service.ProjectContentIdentity(ctx, sessionID, capability)
	if err != nil {
		return nil, err
	}
	origin, ticket, err := service.isolatedRuntimeAccess(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	entry, cleanupURL := origin+"/__retrom/bootstrap", any(nil)
	if source.contentKind == "TYRANOSCRIPT_PROJECT" {
		entry = origin + "/__retrom/tyranoscript/bootstrap"
		cleanupURL = origin + "/__retrom/tyranoscript/cleanup"
	}
	return map[string]any{
		"kind": kind, "origin": origin, "entryUrl": entry, "bootstrapTicket": ticket,
		"cleanupUrl": cleanupURL, "contentDigest": identity,
	}, nil
}

func providerBlobResource(
	source providerConfigSource,
	kind string,
	files []lockedProviderFile,
) (map[string]any, error) {
	selected := lockedProviderFile{}
	for _, file := range files {
		if kind == "SEEKABLE_BLOB" && file.logicalName == rpgMKXPArchiveName {
			selected = file
			break
		}
		if kind != "SEEKABLE_BLOB" && !strings.HasPrefix(file.logicalName, "__retrom__/") {
			selected = file
			break
		}
	}
	if selected.logicalName == "" || selected.size < 1 {
		return nil, ErrCredential
	}
	identity, err := ContentIdentity(ContentView{
		Digest: selected.digest, Format: selected.format, CoreID: source.coreID,
		ProviderID: source.providerID, TargetID: source.targetID,
		BundleSHA256: source.bundleDigest,
		DOSEntry:     nullableStringPointer(source.dosEntry),
	})
	if err != nil {
		return nil, err
	}
	url, err := RuntimeContentURL("game", identity, selected.logicalName)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kind": kind, "url": url, "sha256": selected.digest, "sizeBytes": selected.size,
		"rangeRequired": kind == "SEEKABLE_BLOB",
	}, nil
}

func providerSeekableProjectResource(
	projectIdentity string,
	files []lockedProviderFile,
) (map[string]any, error) {
	selected := lockedProviderFile{}
	for _, file := range files {
		if file.logicalName == rpgMKXPArchiveName {
			selected = file
			break
		}
	}
	if selected.logicalName == "" || selected.size < 1 || !validContentDigest(selected.digest) {
		return nil, ErrCredential
	}
	root, err := RuntimeProjectContentRoot(projectIdentity)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kind": "SEEKABLE_BLOB", "url": root + rpgMKXPArchivePublicName,
		"sha256": selected.digest, "sizeBytes": selected.size, "rangeRequired": true,
	}, nil
}

func (service *Service) providerBundleResource(
	ctx context.Context,
	launchID, capability, role, urlKind, resourceKind string,
) (map[string]any, error) {
	files, err := service.BundleFiles(ctx, launchID, capability, role)
	if err != nil {
		files, err = service.ReviewPreviewBundleFiles(ctx, launchID, capability, role)
	}
	if err != nil || len(files) == 0 || resourceKind != "BIOS_BUNDLE" {
		return nil, ErrCredential
	}
	identity, err := BundleIdentity(files)
	if err != nil {
		return nil, err
	}
	url, err := RuntimeContentURL(urlKind, identity, "bundle.zip")
	if err != nil {
		return nil, err
	}
	return map[string]any{"kind": resourceKind, "files": []map[string]any{{
		"logicalName": "bundle.zip", "virtualPath": "bundle.zip", "url": url,
		"sha256": identity, "sizeBytes": int64(len(files)),
	}}}, nil
}

func (service *Service) providerParentResource(
	ctx context.Context,
	launchID, capability, resourceKind string,
) (map[string]any, error) {
	files, err := service.BundleFiles(ctx, launchID, capability, "PARENT")
	if err != nil {
		files, err = service.ReviewPreviewBundleFiles(ctx, launchID, capability, "PARENT")
	}
	if err != nil || len(files) == 0 || resourceKind != "PARENT_ARCHIVE" {
		return nil, ErrCredential
	}
	identity, err := BundleIdentity(files)
	if err != nil {
		return nil, err
	}
	url, err := RuntimeContentURL("parent", identity, "bundle.zip")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kind": resourceKind, "url": url, "sha256": identity,
		"sizeBytes": int64(len(files)), "rangeRequired": true,
	}, nil
}

func (service *Service) providerExternalResource(
	ctx context.Context,
	launchID, resourceKind string,
) (map[string]any, error) {
	if resourceKind != "EXTERNAL_FILE_SET" {
		return nil, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.virtual_path,file.logical_name,blob.sha256,blob.size_bytes
FROM launch_external_files file JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=? AND file.kind='BIOS'
UNION ALL
SELECT file.virtual_path,file.logical_name,blob.sha256,blob.size_bytes
FROM review_preview_files file JOIN blobs blob ON blob.id=file.blob_id
WHERE file.preview_session_id=? AND file.role='EXTERNAL_FILE'
ORDER BY 1
	`, launchID, launchID)
	if err != nil {
		return nil, fmt.Errorf("load Provider external files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]map[string]any, 0)
	for rows.Next() {
		var virtualPath, logicalName, digest string
		var size int64
		if err := rows.Scan(&virtualPath, &logicalName, &digest, &size); err != nil {
			return nil, fmt.Errorf("scan Provider external file: %w", err)
		}
		identity, err := ExternalContentIdentity(digest)
		if err != nil {
			return nil, err
		}
		url, err := RuntimeContentURL("external", identity, logicalName)
		if err != nil {
			return nil, err
		}
		files = append(files, map[string]any{
			"logicalName": logicalName, "virtualPath": strings.TrimPrefix(virtualPath, "/"),
			"url": url, "sha256": digest, "sizeBytes": size,
		})
	}
	if len(files) == 0 {
		return nil, ErrCredential
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Provider external files: %w", err)
	}
	return map[string]any{"kind": resourceKind, "files": files}, nil
}

func (service *Service) providerDiscResource(
	ctx context.Context,
	launchID string,
	initialDisc int64,
	resourceKind string,
) (map[string]any, error) {
	if resourceKind != "MULTI_DISC" {
		return nil, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.logical_name,blob.sha256,blob.size_bytes
FROM launch_external_files file JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=? AND file.kind='DISC'
UNION ALL
SELECT file.logical_name,blob.sha256,blob.size_bytes
FROM review_preview_files file JOIN blobs blob ON blob.id=file.blob_id
WHERE file.preview_session_id=? AND file.role='DISC'
ORDER BY 1
	`, launchID, launchID)
	if err != nil {
		return nil, fmt.Errorf("load Provider disc files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]map[string]any, 0, 8)
	for rows.Next() {
		var logicalName, digest string
		var size int64
		if err := rows.Scan(&logicalName, &digest, &size); err != nil {
			return nil, fmt.Errorf("scan Provider disc file: %w", err)
		}
		identity, err := ExternalContentIdentity(digest)
		if err != nil {
			return nil, err
		}
		url, err := RuntimeContentURL("external", identity, logicalName)
		if err != nil {
			return nil, err
		}
		entries = append(entries, map[string]any{
			"index": len(entries), "label": fmt.Sprintf("光盘 %d", len(entries)+1),
			"url": url, "sha256": digest, "sizeBytes": size,
		})
	}
	if len(entries) < 2 || initialDisc < 0 || initialDisc >= int64(len(entries)) {
		return nil, ErrCredential
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Provider disc files: %w", err)
	}
	return map[string]any{
		"kind": resourceKind, "initialDiscIndex": initialDisc, "entries": entries,
	}, nil
}

func (service *Service) providerRuntimePackResources(
	ctx context.Context,
	launchID, capability, resourceKind string,
) ([]map[string]any, error) {
	identity, err := service.ProjectContentIdentity(ctx, launchID, capability)
	if err != nil {
		return nil, err
	}
	root, err := RuntimeProjectContentRoot(identity)
	if err != nil {
		return nil, err
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.logical_name,blob.sha256,blob.size_bytes FROM launch_content_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=? AND file.logical_name GLOB '__retrom__/pack-?.zip'
ORDER BY file.logical_name
	`, launchID)
	if err != nil {
		return nil, fmt.Errorf("load Provider runtime packs: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	resources := make([]map[string]any, 0, 4)
	for rows.Next() {
		var logicalName, digest string
		var size int64
		if err := rows.Scan(&logicalName, &digest, &size); err != nil {
			return nil, fmt.Errorf("scan Provider runtime pack: %w", err)
		}
		switch resourceKind {
		case "SEEKABLE_BLOB":
			resources = append(resources, map[string]any{
				"kind": resourceKind, "url": root + logicalName, "sha256": digest,
				"sizeBytes": size, "rangeRequired": true,
			})
		case "FILE_TREE":
			slot := strings.TrimSuffix(strings.TrimPrefix(logicalName, "__retrom__/pack-"), ".zip")
			resources = append(resources, map[string]any{
				"kind": resourceKind, "indexUrl": root + "__retrom__/packs/" + slot + "/index.json",
				"contentDigest": digest,
			})
		default:
			return nil, ErrCredential
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Provider runtime packs: %w", err)
	}
	return resources, nil
}

func (service *Service) providerRestore(
	ctx context.Context,
	launchID string,
	source providerConfigSource,
	target runtimebundle.Target,
) (any, bool, error) {
	if source.validationID.Valid {
		var format, digest string
		var size int64
		err := service.database.QueryRowContext(ctx, `
SELECT checkpoint.checkpoint_format,checkpoint.payload_sha256,checkpoint.size_bytes
FROM rpgmaker_runtime_validations validation
JOIN rpgmaker_runtime_validation_checkpoints checkpoint ON checkpoint.validation_id=validation.id
WHERE validation.id=? AND validation.restore_launch_id=?
`, source.validationID.String, launchID).Scan(&format, &digest, &size)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil || target.Checkpoint == nil || !slices.Contains(target.Checkpoint.ReadFormats, format) {
			return nil, false, ErrCredential
		}
		return map[string]any{
			"url": "/runtime/launches/" + launchID + "/state", "format": format,
			"sha256": digest, "sizeBytes": size,
		}, true, nil
	}
	if !source.saveID.Valid {
		return nil, false, nil
	}
	var format, digest string
	var size int64
	if err := service.database.QueryRowContext(ctx, `
SELECT save.checkpoint_format,save.payload_sha256,save.payload_size_bytes
FROM save_states save
WHERE save.id=? AND save.deleted_at_ms IS NULL
	`, source.saveID.String).Scan(&format, &digest, &size); err != nil {
		return nil, false, ErrCredential
	}
	if target.Checkpoint == nil || !slices.Contains(target.Checkpoint.ReadFormats, format) {
		return nil, false, ErrCredential
	}
	return map[string]any{
		"url": "/runtime/launches/" + launchID + "/state", "format": format,
		"sha256": digest, "sizeBytes": size,
	}, true, nil
}

func (service *Service) providerNetplay(source providerConfigSource) (any, string, error) {
	if !source.netplayID.Valid {
		return nil, "SINGLE", nil
	}
	if !source.netplayRoom.Valid || !source.netplayProfile.Valid || !source.netplayPlayer.Valid {
		return nil, "", ErrCredential
	}
	var profile map[string]any
	if err := json.Unmarshal([]byte(source.netplayProfile.String), &profile); err != nil {
		return nil, "", ErrCredential
	}
	socketURL, err := service.netplaySocketURL(source.netplayRoom.String)
	if err != nil {
		return nil, "", err
	}
	return map[string]any{
		"roomId": source.netplayRoom.String, "sessionId": source.netplayID.String,
		"playerNo":  source.netplayPlayer.Int64,
		"socketUrl": socketURL,
		"profile":   profile,
	}, "NETPLAY", nil
}

func (service *Service) activateLaunch(ctx context.Context, launchID, state string) error {
	if state != "CREATED" {
		return nil
	}
	now := service.now().UnixMilli()
	if _, err := service.database.ExecContext(ctx, `
UPDATE launch_sessions SET state='ACTIVE',activated_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=? AND state='CREATED'
`, now, now, launchID); err != nil {
		return fmt.Errorf("activate launch: %w", err)
	}
	return nil
}

func validConfigLifetime(state string, bootstrapEnd, hardEnd int64, idleEnd sql.NullInt64, now int64) bool {
	if hardEnd <= now || state == "FINISHED" || state == "EXPIRED" || state == "REVOKED" {
		return false
	}
	if state == "CREATED" {
		return bootstrapEnd > now
	}
	return state == "ACTIVE" && (!idleEnd.Valid || idleEnd.Int64 > now)
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func (service *Service) MultiDiscTelemetryDimensions(
	ctx context.Context,
	launchID, capability string,
) (MultiDiscTelemetryDimensions, error) {
	var credentialHash []byte
	var state, platformKey, targetKey, bundleDigest, contentFormat string
	var hardEnd int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.hard_expires_at_ms,platform.id,
 launch.target_id,launch.bundle_sha256,content.format_version,
 (SELECT count(*) FROM launch_external_files file WHERE file.launch_session_id=launch.id AND file.kind='DISC')
FROM launch_sessions launch
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN launch_content_files content ON content.launch_session_id=launch.id
WHERE launch.id=? AND content.format_version='RETROM_MULTIDISC_M3U_V1'
`, launchID).Scan(
		&credentialHash, &state, &hardEnd, &platformKey, &targetKey, &bundleDigest, &contentFormat, &discCount,
	)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardEnd <= service.now().UnixMilli() ||
		state != "ACTIVE" || discCount < 2 || discCount > 8 {
		return MultiDiscTelemetryDimensions{}, ErrCredential
	}
	return MultiDiscTelemetryDimensions{
		PlatformKey: platformKey, TargetKey: targetKey, BundleDigest: bundleDigest, DiscCount: discCount,
	}, nil
}

func (service *Service) BundleFiles(ctx context.Context, launchID, capability, kind string) ([]BundleFile, error) {
	if kind != "BIOS_BUNDLE" && kind != "PARENT" {
		return nil, ErrCredential
	}
	var credentialHash []byte
	var state string
	var hardEnd int64
	if err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms FROM launch_sessions WHERE id=?
	`, launchID).Scan(&credentialHash, &state, &hardEnd); err != nil ||
		!retromruntime.MatchesCapability(capability, credentialHash) || state != "ACTIVE" ||
		hardEnd <= service.now().UnixMilli() {
		return nil, ErrCredential
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT file.logical_name,blob.sha256
FROM launch_sessions launch
JOIN launch_external_files file ON file.launch_session_id=launch.id
JOIN blobs blob ON blob.id=file.blob_id
WHERE launch.id=? AND file.kind=? ORDER BY file.logical_name
	`, launchID, kind)
	if err != nil {
		return nil, fmt.Errorf("load launch bundle files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]BundleFile, 0)
	for rows.Next() {
		var file BundleFile
		if err := rows.Scan(&file.LogicalName, &file.SHA256); err != nil {
			return nil, fmt.Errorf("scan launch bundle file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate launch bundle files: %w", err)
	}
	return files, nil
}
