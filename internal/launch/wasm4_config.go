package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	retromruntime "retrom/internal/runtime"
)

const wasm4CartMaxBytes int64 = 1 << 16

type WASM4AdapterConfig struct {
	AdapterKind    string `json:"adapterKind"`
	AdapterID      string `json:"adapterId"`
	CartURL        string `json:"cartUrl"`
	RuntimeBaseURL string `json:"runtimeBaseUrl"`
}

type WASM4Config struct {
	RuntimeFamily   string               `json:"runtimeFamily"`
	ProtocolVersion int                  `json:"protocolVersion"`
	Mode            string               `json:"mode"`
	Purpose         string               `json:"purpose"`
	LaunchID        string               `json:"launchId"`
	SessionID       string               `json:"sessionId"`
	CoreID          string               `json:"coreId"`
	CoreName        string               `json:"coreName"`
	GameTitle       string               `json:"gameTitle"`
	PlatformName    string               `json:"platformName"`
	RuntimeVersion  string               `json:"runtimeVersion"`
	ArtifactID      string               `json:"artifactId"`
	ContentDigest   string               `json:"contentDigest"`
	CartSizeBytes   int64                `json:"cartSizeBytes"`
	ReturnTo        string               `json:"returnTo"`
	Warnings        []string             `json:"warnings"`
	Adapter         WASM4AdapterConfig   `json:"adapter"`
	Checkpoint      *WASM4Checkpoint     `json:"checkpoint"`
	ReviewPreview   *ReviewPreviewConfig `json:"reviewPreview,omitempty"`
}

type WASM4Checkpoint struct {
	PayloadKind string `json:"payloadKind"`
	PayloadURL  string `json:"payloadUrl"`
}

type wasm4Compatibility struct {
	AdapterABI            string   `json:"adapterAbi"`
	CartMaxBytes          int64    `json:"cartMaxBytes"`
	GameCompatibilityLine string   `json:"gameCompatibilityLine"`
	JSPath                string   `json:"jsPath"`
	ReadableSaveABIs      []string `json:"readableSaveAbis"`
	SaveABI               string   `json:"saveAbi"`
	SchemaVersion         int      `json:"schemaVersion"`
	SupportedContentKinds []string `json:"supportedContentKinds"`
}

func (service *Service) prepareWASM4Launch(
	ctx context.Context,
	selection launchSelection,
) (launchPreparation, error) {
	if selection.contentKind != "SINGLE_FILE" || !strings.EqualFold(path.Ext(selection.contentLogicalName), ".wasm") {
		return launchPreparation{}, ErrBlocked
	}
	blobID, logicalName, format, err := service.lockLaunchContent(
		ctx, selection.variantRevisionID, selection.selectedCore,
	)
	if err != nil || logicalName != selection.contentLogicalName || format != "SOURCE_V1" {
		return launchPreparation{}, ErrBlocked
	}
	var sizeBytes int64
	if err := service.database.QueryRowContext(ctx, `SELECT size_bytes FROM blobs WHERE id=?`, blobID).
		Scan(&sizeBytes); err != nil || sizeBytes < 1 || sizeBytes > wasm4CartMaxBytes {
		return launchPreparation{}, ErrBlocked
	}
	return launchPreparation{contentPlan: launchContentPlan{
		ContentKind: "SINGLE_FILE",
		Files:       []lockedContentFile{{BlobID: blobID, LogicalName: logicalName, Format: format}},
	}}, nil
}

func parseWASM4Compatibility(raw string) (wasm4Compatibility, error) {
	var value wasm4Compatibility
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return wasm4Compatibility{}, ErrCredential
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || value.AdapterABI != "wasm4-state-v1" ||
		value.CartMaxBytes != wasm4CartMaxBytes || value.GameCompatibilityLine != "wasm4-v1" ||
		value.JSPath != "wasm4-retrom.mjs" || value.SaveABI != "wasm4-state-v1" ||
		value.SchemaVersion != 5 || !slices.Equal(value.ReadableSaveABIs, []string{"wasm4-state-v1"}) ||
		!slices.Equal(value.SupportedContentKinds, []string{"SINGLE_FILE"}) {
		return wasm4Compatibility{}, ErrCredential
	}
	return value, nil
}

func (service *Service) wasm4RuntimeAvailable(runtimeVersion string, compatibility wasm4Compatibility) bool {
	_, _, exists := service.dependencies.RetromRuntimeFile(runtimeVersion, compatibility.JSPath)
	return exists
}

func (service *Service) validateWASM4ReviewPreviewSource(source reviewPreviewSource) error {
	compatibility, err := parseWASM4Compatibility(source.CompatibilityJSON)
	if err != nil || source.RuntimeFamily != "WASM4" || source.AdapterKind != "WASM4_WEB" ||
		source.AdapterID != "wasm4-web" || source.CoreID != "wasm4" || source.ContentKind != "SINGLE_FILE" ||
		!service.wasm4RuntimeAvailable(source.RuntimeVersion, compatibility) {
		return ErrReviewPreviewUnavailable
	}
	return nil
}

func (service *Service) buildWASM4ReviewConfig(
	previewID string,
	source reviewPreviewConfigSource,
) (Config, error) {
	compatibility, err := parseWASM4Compatibility(source.CompatibilityJSON)
	if err != nil || source.RuntimeFamily != "WASM4" || source.AdapterKind != "WASM4_WEB" ||
		source.AdapterID != "wasm4-web" || source.CoreID != "wasm4" || source.ContentFormat != "SOURCE_V1" ||
		!strings.EqualFold(path.Ext(source.LogicalName), ".wasm") || source.RelativePath != compatibility.JSPath ||
		source.ContentSizeBytes < 1 || source.ContentSizeBytes > compatibility.CartMaxBytes ||
		!service.wasm4RuntimeAvailable(source.RuntimeVersion, compatibility) {
		return Config{}, ErrCredential
	}
	cartURL, err := wasm4ContentURL(ContentView{
		Digest: source.ContentDigest, Format: source.ContentFormat, CoreID: source.CoreID,
	}, source.LogicalName)
	if err != nil {
		return Config{}, ErrCredential
	}
	configuration := WASM4Config{
		RuntimeFamily: "WASM4", ProtocolVersion: 1, Mode: "single", Purpose: "REVIEW_PREVIEW",
		LaunchID: previewID, SessionID: previewID, CoreID: source.CoreID, CoreName: source.CoreName,
		GameTitle: source.Title, PlatformName: source.PlatformName, RuntimeVersion: source.RuntimeVersion,
		ArtifactID: source.ArtifactID, ContentDigest: source.ContentDigest,
		CartSizeBytes: source.ContentSizeBytes, ReturnTo: "/admin/reviews/" + source.ItemID,
		Warnings: []string{"REVIEW_PREVIEW_BEST_EFFORT"},
		Adapter: WASM4AdapterConfig{
			AdapterKind: source.AdapterKind, AdapterID: source.AdapterID, CartURL: cartURL,
			RuntimeBaseURL: "/runtime/retrom-runtime/" + source.RuntimeVersion + "/",
		},
		ReviewPreview: &ReviewPreviewConfig{
			ImportItemID: source.ItemID, CaptureAllowed: source.CaptureAllowed == 1,
			CaptureAfterMS: reviewCaptureAfterMS,
		},
	}
	return Config{RuntimeFamily: "WASM4", WASM4: &configuration}, nil
}

type wasm4ProductConfigSource struct {
	credentialHash                                                []byte
	state, coreID, coreName, artifactID, runtimeVersion           string
	adapterKind, adapterID, entryPath, compatibilityJSON          string
	gameTitle, platformName, returnTo, logicalName, contentFormat string
	contentDigest                                                 string
	bootstrapExpires, hardExpires, contentSizeBytes               int64
	idleExpires                                                   sql.NullInt64
	saveStateID                                                   sql.NullString
}

func (service *Service) validWASM4ProductSource(
	source wasm4ProductConfigSource,
	compatibility wasm4Compatibility,
) bool {
	return source.adapterKind == "WASM4_WEB" && source.adapterID == "wasm4-web" &&
		source.coreID == "wasm4" && source.entryPath == compatibility.JSPath &&
		source.contentFormat == "SOURCE_V1" && strings.EqualFold(path.Ext(source.logicalName), ".wasm") &&
		source.contentSizeBytes >= 1 && source.contentSizeBytes <= compatibility.CartMaxBytes &&
		service.wasm4RuntimeAvailable(source.runtimeVersion, compatibility)
}

func (service *Service) wasm4ProductConfig(
	ctx context.Context,
	launchID, capability string,
) (WASM4Config, error) {
	source, err := service.loadWASM4ProductConfigSource(ctx, launchID)
	if err != nil || !retromruntime.MatchesCapability(capability, source.credentialHash) ||
		!validConfigLifetime(source.state, source.bootstrapExpires, source.hardExpires, source.idleExpires,
			service.now().UnixMilli()) {
		return WASM4Config{}, ErrCredential
	}
	compatibility, err := parseWASM4Compatibility(source.compatibilityJSON)
	if err != nil || !service.validWASM4ProductSource(source, compatibility) {
		return WASM4Config{}, ErrCredential
	}
	cartURL, err := wasm4ContentURL(ContentView{
		Digest: source.contentDigest, Format: source.contentFormat, CoreID: source.coreID,
	}, source.logicalName)
	if err != nil {
		return WASM4Config{}, ErrCredential
	}
	var checkpoint *WASM4Checkpoint
	if source.saveStateID.Valid {
		checkpoint, err = service.wasm4CheckpointConfig(ctx, launchID, source.saveStateID.String)
		if err != nil {
			return WASM4Config{}, err
		}
	}
	if err := service.activateRuntimeLaunch(ctx, launchID, source.state); err != nil {
		return WASM4Config{}, err
	}
	return WASM4Config{
		RuntimeFamily: "WASM4", ProtocolVersion: 1, Mode: "single", Purpose: "PRODUCT",
		LaunchID: launchID, SessionID: launchID, CoreID: source.coreID, CoreName: source.coreName,
		GameTitle: source.gameTitle, PlatformName: source.platformName, RuntimeVersion: source.runtimeVersion,
		ArtifactID: source.artifactID, ContentDigest: source.contentDigest,
		CartSizeBytes: source.contentSizeBytes, ReturnTo: source.returnTo, Warnings: []string{},
		Adapter: WASM4AdapterConfig{
			AdapterKind: source.adapterKind, AdapterID: source.adapterID, CartURL: cartURL,
			RuntimeBaseURL: "/runtime/retrom-runtime/" + source.runtimeVersion + "/",
		},
		Checkpoint: checkpoint,
	}, nil
}

func (service *Service) loadWASM4ProductConfigSource(
	ctx context.Context,
	launchID string,
) (wasm4ProductConfigSource, error) {
	var source wasm4ProductConfigSource
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,launch.state,launch.bootstrap_expires_at_ms,
 launch.hard_expires_at_ms,launch.idle_expires_at_ms,artifact.core_id,core.name,
 artifact.id,artifact.runtime_version,artifact.runtime_adapter_kind,artifact.adapter_id,
 artifact.entry_path,artifact.compatibility_json,metadata.title,platform.name,launch.return_to,
 launch.save_state_id,content.logical_name,content.format_version,blob.sha256,blob.size_bytes
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN cores core ON core.id=artifact.core_id
JOIN game_variant_revisions revision ON revision.id=launch.game_variant_revision_id
JOIN core_artifacts bound_artifact ON bound_artifact.id=revision.core_artifact_id
JOIN games game ON game.id=launch.game_id
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN launch_content_files content ON content.launch_session_id=launch.id
JOIN blobs blob ON blob.id=content.blob_id
WHERE launch.id=? AND launch.purpose='PRODUCT' AND artifact.runtime_family='WASM4'
 AND artifact.available_for_launch=1 AND revision.status='READY'
 AND bound_artifact.core_id=artifact.core_id AND bound_artifact.route_key=artifact.route_key
 AND json_extract(bound_artifact.compatibility_json,'$.gameCompatibilityLine')=
     json_extract(artifact.compatibility_json,'$.gameCompatibilityLine')
`, launchID).Scan(
		&source.credentialHash, &source.state, &source.bootstrapExpires, &source.hardExpires,
		&source.idleExpires, &source.coreID, &source.coreName, &source.artifactID,
		&source.runtimeVersion, &source.adapterKind, &source.adapterID, &source.entryPath,
		&source.compatibilityJSON, &source.gameTitle, &source.platformName, &source.returnTo,
		&source.saveStateID, &source.logicalName, &source.contentFormat, &source.contentDigest,
		&source.contentSizeBytes,
	)
	if err != nil {
		return wasm4ProductConfigSource{}, ErrCredential
	}
	return source, nil
}

func wasm4ContentURL(content ContentView, logicalName string) (string, error) {
	identity, err := ContentIdentity(content)
	if err != nil {
		return "", err
	}
	return RuntimeContentURL("game", identity, logicalName)
}

func (service *Service) wasm4CheckpointConfig(
	ctx context.Context,
	launchID, saveStateID string,
) (*WASM4Checkpoint, error) {
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
	if errors.Is(err, sql.ErrNoRows) || payloadKind != "RUNTIME_STATE" {
		return nil, ErrCredential
	}
	if err != nil {
		return nil, fmt.Errorf("load WASM-4 product checkpoint: %w", err)
	}
	return &WASM4Checkpoint{
		PayloadKind: payloadKind, PayloadURL: "/runtime/launches/" + launchID + "/state",
	}, nil
}
