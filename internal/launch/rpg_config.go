package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/dependencies"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/routing"
	rpgvalidation "retrom/internal/rpgmaker/validation"
	retromruntime "retrom/internal/runtime"
)

type RPGMakerConfig struct {
	RuntimeFamily          string                 `json:"runtimeFamily"`
	ProtocolVersion        int                    `json:"protocolVersion"`
	Mode                   string                 `json:"mode"`
	Purpose                string                 `json:"purpose"`
	LaunchID               string                 `json:"launchId"`
	CoreID                 string                 `json:"coreId"`
	CoreName               string                 `json:"coreName"`
	GameTitle              string                 `json:"gameTitle"`
	PlatformName           string                 `json:"platformName"`
	ReturnTo               string                 `json:"returnTo"`
	Warnings               []string               `json:"warnings"`
	Generation             string                 `json:"generation"`
	RouteKey               string                 `json:"routeKey"`
	ArtifactID             string                 `json:"artifactId"`
	Checkpoint             *RPGCheckpointRestore  `json:"checkpoint"`
	CheckpointAvailability CheckpointAvailability `json:"checkpointAvailability"`
	RuntimeValidation      *RPGValidationResume   `json:"runtimeValidation"`
	Adapter                any                    `json:"adapter"`
}

type RPGValidationResume struct {
	ValidationID              string                     `json:"validationId"`
	State                     string                     `json:"state"`
	OriginalLaunchID          string                     `json:"originalLaunchId"`
	RestoreLaunchID           *string                    `json:"restoreLaunchId"`
	LastGateSequence          int64                      `json:"lastGateSequence"`
	MachineGates              []RPGValidationMachineGate `json:"machineGates"`
	CheckpointEvidence        *RPGValidationCheckpoint   `json:"checkpointEvidence"`
	RestoreScreenshotUploaded bool                       `json:"restoreScreenshotUploaded"`
}

type RPGValidationMachineGate struct {
	Gate          string          `json:"gate"`
	Status        string          `json:"status"`
	BegunAtMS     *int64          `json:"begunAtMs"`
	CompletedAtMS *int64          `json:"completedAtMs"`
	Evidence      json.RawMessage `json:"evidence"`
	FailureCode   *string         `json:"failureCode"`
}

type RPGValidationCheckpoint struct {
	PayloadKind string `json:"payloadKind"`
	SizeBytes   int64  `json:"sizeBytes"`
	SHA256      string `json:"sha256"`
}

type RPGCheckpointRestore struct {
	PayloadKind string `json:"payloadKind"`
	PayloadURL  string `json:"payloadUrl"`
}

type CheckpointAvailability struct {
	Available bool    `json:"available"`
	Reason    *string `json:"reason"`
}

type EasyRPGAdapterConfig struct {
	AdapterKind     string             `json:"adapterKind"`
	AdapterID       string             `json:"adapterId"`
	EngineMode      string             `json:"engineMode"`
	RuntimeBaseURL  string             `json:"runtimeBaseUrl"`
	ProjectRootURL  string             `json:"projectRootUrl"`
	ProjectIndexURL string             `json:"projectIndexUrl"`
	RTPSource       *RPGFileTreeSource `json:"rtpSource"`
	CheckpointSlot  int                `json:"checkpointSlot"`
}

type RPGFileTreeSource struct {
	Kind     string `json:"kind"`
	IndexURL string `json:"indexUrl"`
}

type RPGSeekableBlobSource struct {
	Slot          int    `json:"-"`
	Kind          string `json:"kind"`
	RangeRequired bool   `json:"rangeRequired"`
	DeclaredName  string `json:"declaredName,omitempty"`
	URL           string `json:"url"`
	SHA256        string `json:"sha256"`
	SizeBytes     int64  `json:"sizeBytes"`
}

type MKXPCoreConfig struct {
	JSURL             string `json:"jsUrl"`
	JSSizeBytes       int64  `json:"jsSizeBytes"`
	JSSHA256          string `json:"jsSha256"`
	WasmURL           string `json:"wasmUrl"`
	WasmSizeBytes     int64  `json:"wasmSizeBytes"`
	WasmSHA256        string `json:"wasmSha256"`
	ArtifactSetSHA256 string `json:"artifactSetSha256"`
}

type MKXPAdapterConfig struct {
	AdapterKind      string                  `json:"adapterKind"`
	AdapterID        string                  `json:"adapterId"`
	Core             MKXPCoreConfig          `json:"core"`
	RuntimeBaseURL   string                  `json:"runtimeBaseUrl"`
	ProjectArchive   RPGSeekableBlobSource   `json:"projectArchive"`
	RTPArchives      []RPGSeekableBlobSource `json:"rtpArchives"`
	RGSSVersion      int                     `json:"rgssVersion"`
	StateBufferBytes int                     `json:"stateBufferBytes"`
}

type NativeWebAdapterConfig struct {
	AdapterKind     string `json:"adapterKind"`
	AdapterID       string `json:"adapterId"`
	BridgeProfile   string `json:"bridgeProfile"`
	UniqueOrigin    string `json:"uniqueOrigin"`
	BootstrapURL    string `json:"bootstrapUrl"`
	BootstrapTicket string `json:"bootstrapTicket"`
}

func (configuration Config) MarshalJSON() ([]byte, error) {
	if configuration.RPGMaker != nil {
		contents, err := json.Marshal(configuration.RPGMaker)
		if err != nil {
			return nil, fmt.Errorf("marshal RPG Maker launch config: %w", err)
		}
		return contents, nil
	}
	if configuration.ONS != nil {
		contents, err := json.Marshal(configuration.ONS)
		if err != nil {
			return nil, fmt.Errorf("marshal ONS launch config: %w", err)
		}
		return contents, nil
	}
	if configuration.KiriKiri != nil {
		contents, err := json.Marshal(configuration.KiriKiri)
		if err != nil {
			return nil, fmt.Errorf("marshal KiriKiri launch config: %w", err)
		}
		return contents, nil
	}
	type plainConfig Config
	contents, err := json.Marshal(plainConfig(configuration))
	if err != nil {
		return nil, fmt.Errorf("marshal EmulatorJS launch config: %w", err)
	}
	return contents, nil
}

func (service *Service) Config(ctx context.Context, launchID, capability string) (Config, error) {
	var family string
	if err := service.database.QueryRowContext(ctx, `
SELECT artifact.runtime_family FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
WHERE launch.id=? AND artifact.available_for_launch=1
`, launchID).Scan(&family); err != nil {
		return Config{}, ErrCredential
	}
	if family == "EMULATORJS" {
		return service.emulatorJSConfig(ctx, launchID, capability)
	}
	if family == "RPGMAKER" {
		rpg, err := service.rpgMakerConfig(ctx, launchID, capability)
		if err != nil {
			return Config{}, err
		}
		return Config{RuntimeFamily: "RPGMAKER", RPGMaker: &rpg}, nil
	}
	if family == "ONS" {
		ons, err := service.onsProductConfig(ctx, launchID, capability)
		if err != nil {
			return Config{}, err
		}
		return Config{RuntimeFamily: "ONS", ONS: &ons}, nil
	}
	if family == "KIRIKIRI" {
		kirikiri, err := service.kiriKiriProductConfig(ctx, launchID, capability)
		if err != nil {
			return Config{}, err
		}
		return Config{RuntimeFamily: "KIRIKIRI", KiriKiri: &kirikiri}, nil
	}
	return Config{}, ErrCredential
}

type rpgConfigSource struct {
	credentialHash                                          []byte
	state, purpose, coreID, coreName, artifactID, routeKey  string
	runtimeKind, runtimeVersion, adapterID, entryPath       string
	artifactSetSHA, compatibilityJSON, generation, returnTo string
	gameTitle                                               string
	bootstrapExpires, hardExpires, createdAt                int64
	idleExpires                                             sql.NullInt64
	saveStateID, validationID                               sql.NullString
}

func (service *Service) rpgMakerConfig(
	ctx context.Context,
	launchID, capability string,
) (RPGMakerConfig, error) {
	source, err := service.authorizedRPGConfigSource(ctx, launchID, capability)
	if err != nil {
		return RPGMakerConfig{}, ErrCredential
	}
	checkpointValue, checkpointAvailable, err := service.loadRPGCheckpointConfig(ctx, launchID, source)
	if err != nil {
		return RPGMakerConfig{}, ErrCredential
	}
	var checkpoint *RPGCheckpointRestore
	if checkpointAvailable {
		checkpoint = &checkpointValue
	}
	if !validRPGConfigRoute(source) {
		return RPGMakerConfig{}, ErrBlocked
	}
	var validation *RPGValidationResume
	if source.purpose == "RPG_RUNTIME_VALIDATION" {
		validation, err = service.loadRPGValidationResume(ctx, launchID, source)
		if err != nil {
			return RPGMakerConfig{}, ErrCredential
		}
	}
	projectRoot := ""
	if source.runtimeKind != "NATIVE_WEB" {
		var identityErr error
		projectRoot, identityErr = service.ProjectContentRoot(ctx, launchID, capability)
		if identityErr != nil {
			return RPGMakerConfig{}, ErrCredential
		}
	}
	adapter, err := service.buildRPGAdapterConfig(ctx, launchID, source, projectRoot)
	if err != nil {
		return RPGMakerConfig{}, err
	}
	if err := service.activateRuntimeLaunch(ctx, launchID, source.state); err != nil {
		return RPGMakerConfig{}, err
	}
	reason := "RUNTIME_NOT_READY"
	if checkpointAvailable {
		reason = "CHECKPOINT_ALREADY_CREATED"
	}
	return RPGMakerConfig{
		RuntimeFamily: "RPGMAKER", ProtocolVersion: 1, Mode: "single", Purpose: source.purpose,
		LaunchID: launchID, CoreID: source.coreID, CoreName: source.coreName,
		GameTitle: source.gameTitle, PlatformName: "RPG Maker", ReturnTo: source.returnTo,
		Warnings: []string{}, Generation: source.generation, RouteKey: source.routeKey,
		ArtifactID: source.artifactID, Checkpoint: checkpoint,
		CheckpointAvailability: CheckpointAvailability{Available: false, Reason: &reason},
		RuntimeValidation:      validation,
		Adapter:                adapter,
	}, nil
}

func (service *Service) authorizedRPGConfigSource(
	ctx context.Context,
	launchID, capability string,
) (rpgConfigSource, error) {
	source, err := service.loadRPGConfigSource(ctx, launchID)
	if err != nil || !retromruntime.MatchesCapability(capability, source.credentialHash) ||
		!validConfigLifetime(
			source.state, source.bootstrapExpires, source.hardExpires, source.idleExpires,
			service.now().UnixMilli(),
		) {
		return rpgConfigSource{}, ErrCredential
	}
	return source, nil
}

func (service *Service) loadRPGValidationResume(
	ctx context.Context,
	launchID string,
	source rpgConfigSource,
) (*RPGValidationResume, error) {
	if !source.validationID.Valid {
		return nil, ErrCredential
	}
	var resume RPGValidationResume
	var restoreID, screenshotID, payloadKind, payloadSHA sql.NullString
	var payloadSize sql.NullInt64
	var machineJSON string
	err := service.database.QueryRowContext(ctx, `
SELECT validation.id,validation.state,validation.launch_id,validation.restore_launch_id,
 validation.last_gate_sequence,validation.machine_gates_json,validation.evidence_screenshot_blob_id,
 checkpoint.payload_kind,checkpoint.size_bytes,checkpoint.payload_sha256
FROM rpgmaker_runtime_validations validation
LEFT JOIN rpgmaker_runtime_validation_checkpoints checkpoint ON checkpoint.validation_id=validation.id
WHERE validation.id=? AND (validation.launch_id=? OR validation.restore_launch_id=?)
`, source.validationID.String, launchID, launchID).Scan(
		&resume.ValidationID, &resume.State, &resume.OriginalLaunchID, &restoreID,
		&resume.LastGateSequence, &machineJSON, &screenshotID, &payloadKind, &payloadSize, &payloadSHA,
	)
	if err != nil || resume.OriginalLaunchID == "" {
		return nil, ErrCredential
	}
	if restoreID.Valid {
		resume.RestoreLaunchID = &restoreID.String
	}
	resume.RestoreScreenshotUploaded = screenshotID.Valid
	if payloadKind.Valid && payloadSize.Valid && payloadSHA.Valid {
		resume.CheckpointEvidence = &RPGValidationCheckpoint{
			PayloadKind: payloadKind.String, SizeBytes: payloadSize.Int64, SHA256: payloadSHA.String,
		}
	} else if payloadKind.Valid || payloadSize.Valid || payloadSHA.Valid {
		return nil, ErrCredential
	}
	if err := json.Unmarshal([]byte(machineJSON), &resume.MachineGates); err != nil ||
		!validRPGValidationMachineGates(resume.MachineGates) {
		return nil, ErrCredential
	}
	return &resume, nil
}

func validRPGValidationMachineGates(gates []RPGValidationMachineGate) bool {
	order := rpgvalidation.GateOrder()
	if len(gates) != len(order) {
		return false
	}
	for index, gate := range gates {
		if gate.Gate != string(order[index]) ||
			(gate.Status != "NOT_STARTED" && gate.Status != "IN_PROGRESS" &&
				gate.Status != "PASSED" && gate.Status != "FAILED") {
			return false
		}
	}
	return true
}

func validRPGConfigRoute(source rpgConfigSource) bool {
	entry, err := routing.ByRoute(source.coreID, source.routeKey)
	return err == nil && string(entry.Generation) == source.generation &&
		entry.Generation == detector.Generation(source.generation) &&
		string(entry.AdapterKind) == source.runtimeKind && entry.AdapterID == source.adapterID &&
		entry.RuntimeVersion == source.runtimeVersion
}

// RPGNativeBridgeAuthorized returns the immutable bridge coordinates frozen on
// an active native-web Launch. Historical Launches deliberately keep their
// original runtime version instead of following the route selected for new
// bindings.
func (service *Service) RPGNativeBridgeAuthorized(
	ctx context.Context,
	launchID string,
) (string, string, error) {
	source, err := service.loadRPGConfigSource(ctx, launchID)
	if err != nil || source.state != "ACTIVE" || source.hardExpires <= service.now().UnixMilli() ||
		source.runtimeKind != "NATIVE_WEB" || !validRPGConfigRoute(source) {
		return "", "", ErrCredential
	}
	return source.runtimeVersion, source.entryPath, nil
}

func (service *Service) loadRPGConfigSource(ctx context.Context, launchID string) (rpgConfigSource, error) {
	var source rpgConfigSource
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.purpose,artifact.core_id,core.name,
artifact.id,artifact.route_key,artifact.runtime_adapter_kind,artifact.runtime_version,
artifact.adapter_id,artifact.entry_path,artifact.artifact_set_sha256,artifact.compatibility_json,
COALESCE(product_profile.generation,validation.generation),launch.return_to,
COALESCE(metadata.title,'RPG Maker runtime validation'),launch.bootstrap_expires_at_ms,
launch.hard_expires_at_ms,launch.created_at_ms,launch.idle_expires_at_ms,
launch.save_state_id,launch.rpgmaker_runtime_validation_id
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
  AND artifact.runtime_family='RPGMAKER' AND artifact.available_for_launch=1
JOIN cores core ON core.id=artifact.core_id
LEFT JOIN games game ON game.id=launch.game_id
LEFT JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
LEFT JOIN rpgmaker_variant_profiles product_profile
  ON product_profile.game_variant_revision_id=launch.game_variant_revision_id
LEFT JOIN game_variant_revisions product_revision ON product_revision.id=launch.game_variant_revision_id
LEFT JOIN core_artifacts bound_artifact ON bound_artifact.id=product_revision.core_artifact_id
LEFT JOIN rpgmaker_runtime_validations validation
  ON validation.id=launch.rpgmaker_runtime_validation_id
WHERE launch.id=? AND launch.route_key=artifact.route_key
  AND ((launch.purpose='PRODUCT' AND product_profile.game_variant_revision_id IS NOT NULL
      AND product_profile.route_key=launch.route_key
      AND product_revision.game_content_revision_id=launch.game_content_revision_id
      AND bound_artifact.core_id=artifact.core_id AND bound_artifact.route_key=artifact.route_key
      AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
          json_extract(artifact.compatibility_json,'$.gameCompatibilityLine'))
    OR (launch.purpose='RPG_RUNTIME_VALIDATION' AND validation.id IS NOT NULL
      AND validation.route_key=launch.route_key AND validation.artifact_id=artifact.id
      AND validation.artifact_set_sha256=artifact.artifact_set_sha256
      AND validation.adapter_id=artifact.adapter_id))
`, launchID).Scan(
		&source.credentialHash, &source.state, &source.purpose, &source.coreID, &source.coreName,
		&source.artifactID, &source.routeKey, &source.runtimeKind, &source.runtimeVersion,
		&source.adapterID, &source.entryPath, &source.artifactSetSHA, &source.compatibilityJSON,
		&source.generation, &source.returnTo, &source.gameTitle, &source.bootstrapExpires,
		&source.hardExpires, &source.createdAt, &source.idleExpires, &source.saveStateID,
		&source.validationID,
	)
	if err != nil {
		return rpgConfigSource{}, fmt.Errorf("load RPG Maker launch config: %w", err)
	}
	return source, nil
}

func (service *Service) loadRPGCheckpointConfig(
	ctx context.Context,
	launchID string,
	source rpgConfigSource,
) (RPGCheckpointRestore, bool, error) {
	var payloadKind string
	if source.purpose == "PRODUCT" && source.saveStateID.Valid {
		if err := service.database.QueryRowContext(ctx, `
SELECT save.payload_kind FROM save_states save
JOIN launch_sessions launch ON launch.id=?
JOIN save_state_runtime_compatibility compatibility ON compatibility.save_state_id=save.id
WHERE save.id=launch.save_state_id AND save.id=? AND save.deleted_at_ms IS NULL
  AND save.game_content_revision_id=launch.game_content_revision_id
  AND save.game_variant_revision_id=launch.game_variant_revision_id
  AND compatibility.status='AVAILABLE'
`, launchID, source.saveStateID.String).Scan(&payloadKind); err != nil {
			return RPGCheckpointRestore{}, false, fmt.Errorf("load product RPG checkpoint: %w", err)
		}
	} else if source.purpose == "RPG_RUNTIME_VALIDATION" && source.validationID.Valid {
		err := service.database.QueryRowContext(ctx, `
SELECT checkpoint.payload_kind FROM rpgmaker_runtime_validations validation
JOIN rpgmaker_runtime_validation_checkpoints checkpoint ON checkpoint.validation_id=validation.id
WHERE validation.id=? AND validation.restore_launch_id=?
`, source.validationID.String, launchID).Scan(&payloadKind)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return RPGCheckpointRestore{}, false, fmt.Errorf("load validation RPG checkpoint: %w", err)
		}
	}
	if payloadKind == "" {
		return RPGCheckpointRestore{}, false, nil
	}
	return RPGCheckpointRestore{
		PayloadKind: payloadKind, PayloadURL: "/runtime/launches/" + launchID + "/state",
	}, true, nil
}

func (service *Service) buildRPGAdapterConfig(
	ctx context.Context,
	launchID string,
	source rpgConfigSource,
	projectRoot string,
) (any, error) {
	runtimeBase := "/runtime/retrom-runtime/" + source.runtimeVersion + "/"
	switch source.runtimeKind {
	case "EASYRPG_WEB":
		if projectRoot == "" {
			return nil, ErrBlocked
		}
		return service.buildEasyRPGAdapterConfig(ctx, launchID, source, runtimeBase, projectRoot)
	case "MKXP_LIBRETRO_WEB":
		if projectRoot == "" {
			return nil, ErrBlocked
		}
		return service.buildMKXPAdapterConfig(ctx, launchID, source, runtimeBase, projectRoot)
	case "NATIVE_WEB":
		return service.buildNativeWebAdapterConfig(ctx, launchID, source)
	default:
		return nil, ErrBlocked
	}
}

func (service *Service) buildEasyRPGAdapterConfig(
	ctx context.Context,
	launchID string,
	source rpgConfigSource,
	runtimeBase string,
	projectRoot string,
) (EasyRPGAdapterConfig, error) {
	engine := "rpg2k"
	if source.generation == "RPG2003" {
		engine = "rpg2k3"
	} else if source.generation != "RPG2000" {
		return EasyRPGAdapterConfig{}, ErrBlocked
	}
	packs, err := service.rpgRuntimePacks(ctx, launchID, projectRoot)
	if err != nil || len(packs) > 1 {
		return EasyRPGAdapterConfig{}, ErrBlocked
	}
	var pack *RPGFileTreeSource
	if len(packs) == 1 {
		pack = &RPGFileTreeSource{
			Kind:     "FILE_TREE_V1",
			IndexURL: fmt.Sprintf("%s__retrom__/packs/%d/index.json", projectRoot, packs[0].Slot),
		}
	}
	return EasyRPGAdapterConfig{
		AdapterKind: source.runtimeKind, AdapterID: source.adapterID, EngineMode: engine,
		RuntimeBaseURL: runtimeBase, ProjectRootURL: projectRoot,
		ProjectIndexURL: projectRoot + "index.json", RTPSource: pack, CheckpointSlot: 100,
	}, nil
}

func (service *Service) buildMKXPAdapterConfig(
	ctx context.Context,
	launchID string,
	source rpgConfigSource,
	runtimeBase string,
	projectRoot string,
) (MKXPAdapterConfig, error) {
	rgss := map[string]int{"RPGXP": 1, "RPGVX": 2, "RPGVXACE": 3}[source.generation]
	if rgss == 0 {
		return MKXPAdapterConfig{}, ErrBlocked
	}
	jsPath, wasmPath := rpgCorePaths(source.entryPath, source.compatibilityJSON)
	core, coreExists := mkxpCoreConfig(
		service.dependencies, runtimeBase, source.runtimeVersion, jsPath, wasmPath, source.artifactSetSHA,
	)
	if !coreExists {
		return MKXPAdapterConfig{}, ErrBlocked
	}
	packs, err := service.rpgRuntimePacks(ctx, launchID, projectRoot)
	if err != nil || len(packs) > 3 {
		return MKXPAdapterConfig{}, ErrBlocked
	}
	archive, err := service.rpgLockedArchive(ctx, launchID, rpgMKXPArchiveName, projectRoot)
	if err != nil {
		return MKXPAdapterConfig{}, err
	}
	return MKXPAdapterConfig{
		AdapterKind: source.runtimeKind, AdapterID: source.adapterID, Core: core,
		RuntimeBaseURL: runtimeBase, ProjectArchive: archive, RTPArchives: packs,
		RGSSVersion: rgss, StateBufferBytes: 268435456,
	}, nil
}

func (service *Service) buildNativeWebAdapterConfig(
	ctx context.Context,
	launchID string,
	source rpgConfigSource,
) (NativeWebAdapterConfig, error) {
	profile := map[string]string{"RPGMV": "RPGMV", "RPGMZ": "RPGMZ"}[source.generation]
	origin, ticket, err := service.nativeRuntimeAccess(ctx, launchID)
	if err != nil || profile == "" {
		return NativeWebAdapterConfig{}, ErrBlocked
	}
	return NativeWebAdapterConfig{
		AdapterKind: source.runtimeKind, AdapterID: source.adapterID, BridgeProfile: profile,
		UniqueOrigin: origin, BootstrapURL: origin + "/__retrom/bootstrap", BootstrapTicket: ticket,
	}, nil
}

func mkxpCoreConfig(
	set *dependencies.Set,
	runtimeBase, runtimeVersion, jsPath, wasmPath, artifactSetSHA string,
) (MKXPCoreConfig, bool) {
	_, jsFile, jsExists := set.RetromRuntimeFile(runtimeVersion, jsPath)
	_, wasmFile, wasmExists := set.RetromRuntimeFile(runtimeVersion, wasmPath)
	if !jsExists || !wasmExists || jsFile.Role != "runtime_js" || wasmFile.Role != "runtime_wasm" {
		return MKXPCoreConfig{}, false
	}
	return MKXPCoreConfig{
		JSURL: runtimeBase + jsPath, JSSizeBytes: jsFile.SizeBytes, JSSHA256: jsFile.SHA256,
		WasmURL: runtimeBase + wasmPath, WasmSizeBytes: wasmFile.SizeBytes, WasmSHA256: wasmFile.SHA256,
		ArtifactSetSHA256: artifactSetSHA,
	}, true
}

func rpgCorePaths(entryPath, compatibilityJSON string) (string, string) {
	var compatibility struct {
		JSPath   string `json:"jsPath"`
		WasmPath string `json:"wasmPath"`
	}
	_ = json.Unmarshal([]byte(compatibilityJSON), &compatibility)
	if compatibility.JSPath == "" {
		compatibility.JSPath = entryPath
	}
	if compatibility.WasmPath == "" {
		compatibility.WasmPath = strings.TrimSuffix(compatibility.JSPath, path.Ext(compatibility.JSPath)) + ".wasm"
	}
	return compatibility.JSPath, compatibility.WasmPath
}

func (service *Service) activateRuntimeLaunch(
	ctx context.Context,
	launchID string,
	state string,
) error {
	if state != "CREATED" {
		return nil
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("activate runtime launch: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='ACTIVE',activated_at_ms=?,updated_at_ms=?,version=version+1
WHERE id=? AND state='CREATED'
`, now, now, launchID)
	if err != nil {
		return fmt.Errorf("activate runtime launch: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrCredential
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("activate runtime launch: %w", err)
	}
	return nil
}

func (service *Service) nativeRuntimeAccess(
	ctx context.Context,
	launchID string,
) (string, string, error) {
	origin, ticket, ticketHash, err := service.nativeRuntimeTicket(launchID)
	if err != nil {
		return "", "", err
	}
	now := service.now().UnixMilli()
	var valid int
	if err := service.database.QueryRowContext(ctx, `
SELECT
 EXISTS(SELECT 1 FROM isolated_runtime_bootstrap_tickets
  WHERE launch_id=? AND ticket_sha256=? AND expected_origin=?
    AND consumed_at_ms IS NULL AND expires_at_ms>?)
 OR EXISTS(SELECT 1 FROM isolated_runtime_capabilities
  WHERE launch_id=? AND expected_origin=? AND revoked_at_ms IS NULL AND expires_at_ms>?)
`, launchID, ticketHash[:], origin, now, launchID, origin, now).Scan(&valid); err != nil || valid != 1 {
		return "", "", ErrBlocked
	}
	return origin, ticket, nil
}

func (service *Service) nativeRuntimeTicket(launchID string) (string, string, [32]byte, error) {
	var empty [32]byte
	if service.rpgRuntimeOriginTemplate == "" || strings.Count(service.rpgRuntimeOriginTemplate, "{launchId}") != 1 {
		return "", "", empty, ErrBlocked
	}
	origin := strings.Replace(service.rpgRuntimeOriginTemplate, "{launchId}", launchID, 1)
	parsed, err := uuid.Parse(launchID)
	if err != nil {
		return "", "", empty, ErrBlocked
	}
	capability := service.credentials.Capability(parsed)
	ticketBytes := sha256.Sum256(append([]byte("retrom-rpg-bootstrap-v1\x00"), capability[:]...))
	ticket := base64.RawURLEncoding.EncodeToString(ticketBytes[:])
	hash := sha256.Sum256(ticketBytes[:])
	return origin, ticket, hash, nil
}

func (service *Service) lockNativeBootstrapTicket(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, profileID, artifactID string,
	createdAt int64,
) error {
	var native int
	if err := transaction.QueryRowContext(ctx, `
SELECT runtime_adapter_kind='NATIVE_WEB' FROM core_artifacts WHERE id=?
`, artifactID).Scan(&native); err != nil {
		return fmt.Errorf("inspect native RPG runtime: %w", err)
	}
	if native != 1 {
		return nil
	}
	origin, _, ticketHash, err := service.nativeRuntimeTicket(launchID)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO isolated_runtime_bootstrap_tickets(
  ticket_sha256,launch_id,profile_id,expected_origin,expires_at_ms,consumed_at_ms)
VALUES(?,?,?,?,?,NULL)
`, ticketHash[:], launchID, profileID, origin, createdAt+60_000); err != nil {
		return fmt.Errorf("lock native RPG bootstrap ticket: %w", err)
	}
	return nil
}

func (service *Service) rpgRuntimePacks(
	ctx context.Context,
	launchID string,
	projectRoot string,
) ([]RPGSeekableBlobSource, error) {
	query := `
SELECT selection.slot,selection.declared_name,installation.bundle_sha256,blob.size_bytes,
locked.logical_name
FROM launch_sessions launch
JOIN game_variant_revision_runtime_packs selection
  ON selection.game_variant_revision_id=launch.game_variant_revision_id
JOIN runtime_asset_pack_installations installation
  ON installation.id=selection.installation_id AND installation.status='READY'
JOIN blobs blob ON blob.id=installation.bundle_blob_id
JOIN launch_content_files locked ON locked.launch_session_id=launch.id
  AND locked.blob_id=installation.bundle_blob_id
WHERE launch.id=? AND launch.purpose='PRODUCT'
UNION ALL
SELECT selection.slot,selection.declared_name,installation.bundle_sha256,blob.size_bytes,
locked.logical_name
FROM launch_sessions launch
JOIN rpgmaker_runtime_validations validation
  ON validation.id=launch.rpgmaker_runtime_validation_id
JOIN review_drafts draft ON draft.import_item_id=validation.import_item_id
JOIN review_draft_runtime_pack_selections selection ON selection.review_draft_id=draft.id
JOIN runtime_asset_pack_installations installation
  ON installation.id=selection.installation_id AND installation.status='READY'
JOIN blobs blob ON blob.id=installation.bundle_blob_id
JOIN launch_content_files locked ON locked.launch_session_id=launch.id
  AND locked.blob_id=installation.bundle_blob_id
WHERE launch.id=? AND launch.purpose='RPG_RUNTIME_VALIDATION'
ORDER BY 1
`
	rows, err := service.database.QueryContext(ctx, query, launchID, launchID)
	if err != nil {
		return nil, fmt.Errorf("load RPG runtime packs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	packs := make([]RPGSeekableBlobSource, 0, 3)
	for rows.Next() {
		var slot int
		var declaredName, digest, logicalName string
		var sizeBytes int64
		if err := rows.Scan(&slot, &declaredName, &digest, &sizeBytes, &logicalName); err != nil ||
			logicalName != fmt.Sprintf("__retrom__/pack-%d.zip", slot) {
			return nil, ErrBlocked
		}
		pack, valid := newRPGSeekableBlobSource(
			projectRoot+logicalName, digest, sizeBytes, declaredName,
		)
		if !valid {
			return nil, ErrBlocked
		}
		pack.Slot = slot
		packs = append(packs, pack)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read RPG runtime packs: %w", err)
	}
	return packs, nil
}

func (service *Service) rpgLockedArchive(
	ctx context.Context,
	launchID, logicalName, projectRoot string,
) (RPGSeekableBlobSource, error) {
	var digest string
	var sizeBytes int64
	if err := service.database.QueryRowContext(ctx, `
SELECT blob.sha256,blob.size_bytes FROM launch_content_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.launch_session_id=? AND file.logical_name=? AND file.format_version='RPG_MAKER_PROJECT_V1'
`, launchID, logicalName).Scan(&digest, &sizeBytes); err != nil {
		return RPGSeekableBlobSource{}, ErrBlocked
	}
	archive, valid := newRPGSeekableBlobSource(projectRoot+logicalName, digest, sizeBytes, "")
	if !valid {
		return RPGSeekableBlobSource{}, ErrBlocked
	}
	return archive, nil
}

func newRPGSeekableBlobSource(
	url, digest string,
	sizeBytes int64,
	declaredName string,
) (RPGSeekableBlobSource, bool) {
	if url == "" || len(digest) != sha256.Size*2 || sizeBytes < 1 {
		return RPGSeekableBlobSource{}, false
	}
	return RPGSeekableBlobSource{
		Kind: "SEEKABLE_BLOB_V1", RangeRequired: true, DeclaredName: declaredName,
		URL: url, SHA256: digest, SizeBytes: sizeBytes,
	}, true
}
