package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"retrom/internal/ons/detector"
	retromruntime "retrom/internal/runtime"
)

const onsProjectFormat = "ONS_PROJECT_V1"

var onsSaveABIIdentity = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,118}-v[1-9][0-9]*$`)

type ONSAdapterConfig struct {
	AdapterKind     string `json:"adapterKind"`
	AdapterID       string `json:"adapterId"`
	RuntimeBaseURL  string `json:"runtimeBaseUrl"`
	ProjectIndexURL string `json:"projectIndexUrl"`
	ScriptEncoding  string `json:"scriptEncoding"`
	CheckpointSlot  int    `json:"checkpointSlot"`
}

type ONSConfig struct {
	RuntimeFamily   string                `json:"runtimeFamily"`
	ProtocolVersion int                   `json:"protocolVersion"`
	Mode            string                `json:"mode"`
	Purpose         string                `json:"purpose"`
	LaunchID        string                `json:"launchId"`
	SessionID       string                `json:"sessionId"`
	CoreID          string                `json:"coreId"`
	CoreName        string                `json:"coreName"`
	GameTitle       string                `json:"gameTitle"`
	PlatformName    string                `json:"platformName"`
	RuntimeVersion  string                `json:"runtimeVersion"`
	ArtifactID      string                `json:"artifactId"`
	ReturnTo        string                `json:"returnTo"`
	Warnings        []string              `json:"warnings"`
	Adapter         ONSAdapterConfig      `json:"adapter"`
	Checkpoint      *ONSCheckpointRestore `json:"checkpoint"`
	ReviewPreview   *ReviewPreviewConfig  `json:"reviewPreview,omitempty"`
}

type ONSCheckpointRestore struct {
	PayloadKind string `json:"payloadKind"`
	PayloadURL  string `json:"payloadUrl"`
}

type onsCompatibility struct {
	AdapterABI            string   `json:"adapterAbi"`
	CheckpointSlot        int      `json:"checkpointSlot"`
	GameCompatibilityLine string   `json:"gameCompatibilityLine"`
	JSPath                string   `json:"jsPath"`
	ReadableSaveABIs      []string `json:"readableSaveAbis"`
	SaveABI               string   `json:"saveAbi"`
	WasmPath              string   `json:"wasmPath"`
}

func (service *Service) buildONSReviewConfig(
	previewID string,
	source reviewPreviewConfigSource,
) (Config, error) {
	profile, err := detector.ParseSnapshot(source.DependencyJSON)
	if err != nil {
		return Config{}, ErrCredential
	}
	compatibility, err := parseONSCompatibility(source.CompatibilityJSON)
	if err != nil || source.RuntimeFamily != "ONS" || source.AdapterKind != "ONS_YURI_WEB" ||
		source.AdapterID != "ons-yuri-web" || source.CoreID != "onscripter_yuri" ||
		source.ContentFormat != onsProjectFormat || source.RelativePath != compatibility.JSPath {
		return Config{}, ErrCredential
	}
	if _, _, exists := service.dependencies.RetromRuntimeFile(source.RuntimeVersion, compatibility.JSPath); !exists {
		return Config{}, ErrCredential
	}
	if _, _, exists := service.dependencies.RetromRuntimeFile(source.RuntimeVersion, compatibility.WasmPath); !exists {
		return Config{}, ErrCredential
	}
	runtimeBase := "/runtime/retrom-runtime/" + source.RuntimeVersion + "/"
	configuration := ONSConfig{
		RuntimeFamily: "ONS", ProtocolVersion: 1, Mode: "single", Purpose: "REVIEW_PREVIEW",
		LaunchID: previewID, SessionID: previewID, CoreID: source.CoreID, CoreName: source.CoreName,
		GameTitle: source.Title, PlatformName: source.PlatformName, RuntimeVersion: source.RuntimeVersion,
		ArtifactID: source.ArtifactID, ReturnTo: "/admin/reviews/" + source.ItemID,
		Warnings: []string{"REVIEW_PREVIEW_BEST_EFFORT"},
		Adapter: ONSAdapterConfig{
			AdapterKind: source.AdapterKind, AdapterID: source.AdapterID, RuntimeBaseURL: runtimeBase,
			ProjectIndexURL: "/runtime/projects/" + previewID + "/index.json",
			ScriptEncoding:  profile.ScriptEncoding, CheckpointSlot: compatibility.CheckpointSlot,
		},
		ReviewPreview: &ReviewPreviewConfig{
			ImportItemID: source.ItemID, CaptureAllowed: source.CaptureAllowed == 1,
			CaptureAfterMS: reviewCaptureAfterMS,
		},
	}
	return Config{RuntimeFamily: "ONS", ONS: &configuration}, nil
}

type onsProductConfigSource struct {
	credentialHash                                                    []byte
	state, coreID, coreName, artifactID, runtimeVersion, adapterKind  string
	adapterID, compatibilityJSON, dependencyJSON, gameTitle, platform string
	returnTo                                                          string
	bootstrapExpires, hardExpires                                     int64
	idleExpires                                                       sql.NullInt64
	saveStateID                                                       sql.NullString
}

func (service *Service) onsProductConfig(
	ctx context.Context,
	launchID, capability string,
) (ONSConfig, error) {
	source, err := service.loadONSProductConfigSource(ctx, launchID)
	if err != nil || !retromruntime.MatchesCapability(capability, source.credentialHash) ||
		!validConfigLifetime(
			source.state, source.bootstrapExpires, source.hardExpires, source.idleExpires,
			service.now().UnixMilli(),
		) {
		return ONSConfig{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(source.dependencyJSON)
	if err != nil || source.adapterKind != "ONS_YURI_WEB" || source.adapterID != "ons-yuri-web" ||
		source.coreID != "onscripter_yuri" {
		return ONSConfig{}, ErrCredential
	}
	compatibility, err := parseONSCompatibility(source.compatibilityJSON)
	if err != nil {
		return ONSConfig{}, ErrCredential
	}
	if _, _, exists := service.dependencies.RetromRuntimeFile(
		source.runtimeVersion, compatibility.JSPath,
	); !exists {
		return ONSConfig{}, ErrBlocked
	}
	if _, _, exists := service.dependencies.RetromRuntimeFile(
		source.runtimeVersion, compatibility.WasmPath,
	); !exists {
		return ONSConfig{}, ErrBlocked
	}
	var checkpoint *ONSCheckpointRestore
	if source.saveStateID.Valid {
		checkpoint, err = service.onsCheckpointConfig(ctx, launchID, source.saveStateID.String)
		if err != nil {
			return ONSConfig{}, err
		}
	}
	if err := service.activateRuntimeLaunch(ctx, launchID, source.state); err != nil {
		return ONSConfig{}, err
	}
	return ONSConfig{
		RuntimeFamily: "ONS", ProtocolVersion: 1, Mode: "single", Purpose: "PRODUCT",
		LaunchID: launchID, SessionID: launchID, CoreID: source.coreID, CoreName: source.coreName,
		GameTitle: source.gameTitle, PlatformName: source.platform, RuntimeVersion: source.runtimeVersion,
		ArtifactID: source.artifactID, ReturnTo: source.returnTo, Warnings: []string{},
		Adapter: ONSAdapterConfig{
			AdapterKind: source.adapterKind, AdapterID: source.adapterID,
			RuntimeBaseURL:  "/runtime/retrom-runtime/" + source.runtimeVersion + "/",
			ProjectIndexURL: "/runtime/projects/" + launchID + "/index.json",
			ScriptEncoding:  profile.ScriptEncoding, CheckpointSlot: compatibility.CheckpointSlot,
		},
		Checkpoint: checkpoint,
	}, nil
}

func (service *Service) loadONSProductConfigSource(
	ctx context.Context,
	launchID string,
) (onsProductConfigSource, error) {
	var source onsProductConfigSource
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.bootstrap_expires_at_ms,
 launch.hard_expires_at_ms,launch.idle_expires_at_ms,artifact.core_id,core.name,
 artifact.id,artifact.runtime_version,artifact.runtime_adapter_kind,artifact.adapter_id,
 artifact.compatibility_json,revision.dependency_snapshot_json,metadata.title,platform.name,
 launch.return_to,launch.save_state_id
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN cores core ON core.id=artifact.core_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
JOIN core_artifacts bound_artifact ON bound_artifact.id=revision.core_artifact_id
JOIN games game ON game.id=launch.game_id
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE launch.id=? AND launch.purpose='PRODUCT' AND artifact.runtime_family='ONS'
 AND artifact.available_for_launch=1 AND revision.status='READY'
 AND bound_artifact.core_id=artifact.core_id AND bound_artifact.route_key=artifact.route_key
 AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
     json_extract(artifact.compatibility_json,'$.gameCompatibilityLine')
`, launchID).Scan(
		&source.credentialHash, &source.state, &source.bootstrapExpires, &source.hardExpires,
		&source.idleExpires, &source.coreID, &source.coreName, &source.artifactID,
		&source.runtimeVersion, &source.adapterKind, &source.adapterID, &source.compatibilityJSON,
		&source.dependencyJSON, &source.gameTitle, &source.platform, &source.returnTo, &source.saveStateID,
	)
	if err != nil {
		return onsProductConfigSource{}, ErrCredential
	}
	return source, nil
}

func (service *Service) onsCheckpointConfig(
	ctx context.Context,
	launchID string,
	saveStateID string,
) (*ONSCheckpointRestore, error) {
	var payloadKind string
	err := service.database.QueryRowContext(ctx, `
SELECT save.payload_kind FROM launch_sessions launch
JOIN save_states save ON save.id=launch.save_state_id
JOIN save_state_runtime_compatibility compatibility ON compatibility.save_state_id=save.id
WHERE launch.id=? AND save.id=? AND save.deleted_at_ms IS NULL
 AND save.game_content_revision_id=launch.game_content_revision_id
 AND save.game_variant_revision_id=launch.game_variant_revision_id
 AND compatibility.status='AVAILABLE'
`, launchID, saveStateID).Scan(&payloadKind)
	if errors.Is(err, sql.ErrNoRows) || payloadKind != "ONS_SAVE_BUNDLE_V1" {
		return nil, ErrCredential
	}
	if err != nil {
		return nil, fmt.Errorf("load ONS product checkpoint: %w", err)
	}
	return &ONSCheckpointRestore{
		PayloadKind: payloadKind, PayloadURL: "/runtime/launches/" + launchID + "/state",
	}, nil
}

func parseONSCompatibility(raw string) (onsCompatibility, error) {
	var value onsCompatibility
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return onsCompatibility{}, ErrCredential
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || value.AdapterABI != "ons-save" ||
		value.CheckpointSlot != 999 || value.GameCompatibilityLine != "onscripter-yuri-v1" ||
		!onsSaveABIIdentity.MatchString(value.SaveABI) || len(value.ReadableSaveABIs) < 1 ||
		len(value.ReadableSaveABIs) > 16 || !slices.Contains(value.ReadableSaveABIs, value.SaveABI) ||
		value.JSPath != "onsyuri.js" ||
		value.WasmPath != "onsyuri.wasm" {
		return onsCompatibility{}, ErrCredential
	}
	return value, nil
}
