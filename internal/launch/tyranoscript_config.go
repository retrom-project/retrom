package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	retromruntime "retrom/internal/runtime"
	"retrom/internal/tyranoscript/detector"
)

const tyranoScriptProjectFormat = "TYRANOSCRIPT_PROJECT_V1"

type TyranoScriptAdapterConfig struct {
	AdapterKind     string  `json:"adapterKind"`
	AdapterID       string  `json:"adapterId"`
	BootstrapTicket string  `json:"bootstrapTicket"`
	CleanupURL      *string `json:"cleanupUrl"`
	EntryURL        string  `json:"entryUrl"`
	UniqueOrigin    string  `json:"uniqueOrigin"`
}

type TyranoScriptConfig struct {
	RuntimeFamily   string                    `json:"runtimeFamily"`
	ProtocolVersion int                       `json:"protocolVersion"`
	Mode            string                    `json:"mode"`
	Purpose         string                    `json:"purpose"`
	LaunchID        string                    `json:"launchId"`
	SessionID       string                    `json:"sessionId"`
	CoreID          string                    `json:"coreId"`
	CoreName        string                    `json:"coreName"`
	GameTitle       string                    `json:"gameTitle"`
	PlatformName    string                    `json:"platformName"`
	RuntimeVersion  string                    `json:"runtimeVersion"`
	ArtifactID      string                    `json:"artifactId"`
	ContentDigest   string                    `json:"contentDigest"`
	ReturnTo        string                    `json:"returnTo"`
	Warnings        []string                  `json:"warnings"`
	Adapter         TyranoScriptAdapterConfig `json:"adapter"`
	Checkpoint      *ButterscotchCheckpoint   `json:"checkpoint"`
	ReviewPreview   *ReviewPreviewConfig      `json:"reviewPreview,omitempty"`
}

type tyranoScriptCompatibility struct {
	AdapterABI            string   `json:"adapterAbi"`
	BridgePath            string   `json:"bridgePath"`
	GameCompatibilityLine string   `json:"gameCompatibilityLine"`
	ReadableSaveABIs      []string `json:"readableSaveAbis"`
	SaveABI               string   `json:"saveAbi"`
}

func (service *Service) buildTyranoScriptReviewConfig(
	ctx context.Context,
	previewID string,
	capability string,
	source reviewPreviewConfigSource,
) (Config, error) {
	if _, err := detector.ParseSnapshot(source.DependencyJSON); err != nil {
		return Config{}, ErrCredential
	}
	compatibility, err := parseTyranoScriptCompatibility(source.CompatibilityJSON)
	if err != nil || source.RuntimeFamily != "TYRANOSCRIPT" || source.AdapterKind != "TYRANOSCRIPT_WEB" ||
		source.AdapterID != "tyranoscript-web" || source.CoreID != "tyranoscript" ||
		source.ContentFormat != tyranoScriptProjectFormat || source.RelativePath != compatibility.BridgePath ||
		!service.tyranoScriptBridgeAvailable(source.RuntimeVersion, compatibility.BridgePath) {
		return Config{}, ErrCredential
	}
	origin, ticket, err := service.isolatedPreviewRuntimeAccess(ctx, previewID)
	if err != nil {
		return Config{}, err
	}
	contentDigest, err := service.ProjectContentIdentity(ctx, previewID, capability)
	if err != nil {
		return Config{}, ErrCredential
	}
	configuration := TyranoScriptConfig{
		RuntimeFamily: "TYRANOSCRIPT", ProtocolVersion: 1, Mode: "single", Purpose: "REVIEW_PREVIEW",
		LaunchID: previewID, SessionID: previewID, CoreID: source.CoreID, CoreName: source.CoreName,
		GameTitle: source.Title, PlatformName: source.PlatformName, RuntimeVersion: source.RuntimeVersion,
		ArtifactID: source.ArtifactID, ContentDigest: contentDigest, ReturnTo: "/admin/reviews/" + source.ItemID,
		Warnings: []string{"REVIEW_PREVIEW_BEST_EFFORT"},
		Adapter:  tyranoScriptAdapter(source.AdapterKind, source.AdapterID, origin, ticket),
		ReviewPreview: &ReviewPreviewConfig{
			ImportItemID: source.ItemID, CaptureAllowed: source.CaptureAllowed == 1,
			CaptureAfterMS: reviewCaptureAfterMS,
		},
	}
	return Config{RuntimeFamily: "TYRANOSCRIPT", TyranoScript: &configuration}, nil
}

func (service *Service) tyranoScriptProductConfig(
	ctx context.Context,
	launchID, capability string,
) (TyranoScriptConfig, error) {
	source, err := service.loadProjectProductConfigSource(ctx, launchID, "TYRANOSCRIPT")
	if err != nil || !retromruntime.MatchesCapability(capability, source.credentialHash) ||
		!validConfigLifetime(source.state, source.bootstrapExpires, source.hardExpires, source.idleExpires,
			service.now().UnixMilli()) {
		return TyranoScriptConfig{}, ErrCredential
	}
	if _, err := detector.ParseSnapshot(source.dependencyJSON); err != nil ||
		source.adapterKind != "TYRANOSCRIPT_WEB" || source.adapterID != "tyranoscript-web" ||
		source.coreID != "tyranoscript" {
		return TyranoScriptConfig{}, ErrCredential
	}
	compatibility, err := parseTyranoScriptCompatibility(source.compatibilityJSON)
	if err != nil || !service.tyranoScriptBridgeAvailable(source.runtimeVersion, compatibility.BridgePath) {
		return TyranoScriptConfig{}, ErrBlocked
	}
	checkpointValue, hasCheckpoint, err := service.tyranoScriptCheckpointConfig(ctx, launchID, source.saveStateID)
	if err != nil {
		return TyranoScriptConfig{}, err
	}
	var checkpoint *ButterscotchCheckpoint
	if hasCheckpoint {
		checkpoint = &checkpointValue
	}
	contentDigest, err := service.ProjectContentIdentity(ctx, launchID, capability)
	if err != nil {
		return TyranoScriptConfig{}, ErrCredential
	}
	origin, ticket, err := service.nativeRuntimeAccess(ctx, launchID)
	if err != nil {
		return TyranoScriptConfig{}, err
	}
	if err := service.activateRuntimeLaunch(ctx, launchID, source.state); err != nil {
		return TyranoScriptConfig{}, err
	}
	return TyranoScriptConfig{
		RuntimeFamily: "TYRANOSCRIPT", ProtocolVersion: 1, Mode: "single", Purpose: "PRODUCT",
		LaunchID: launchID, SessionID: launchID, CoreID: source.coreID, CoreName: source.coreName,
		GameTitle: source.gameTitle, PlatformName: source.platform, RuntimeVersion: source.runtimeVersion,
		ArtifactID: source.artifactID, ContentDigest: contentDigest, ReturnTo: source.returnTo, Warnings: []string{},
		Adapter:    tyranoScriptAdapter(source.adapterKind, source.adapterID, origin, ticket),
		Checkpoint: checkpoint,
	}, nil
}

func tyranoScriptAdapter(kind, id, origin, ticket string) TyranoScriptAdapterConfig {
	cleanupURL := origin + "/__retrom/tyranoscript/cleanup"
	return TyranoScriptAdapterConfig{
		AdapterKind: kind, AdapterID: id, BootstrapTicket: ticket, UniqueOrigin: origin,
		EntryURL: origin + "/__retrom/tyranoscript/bootstrap", CleanupURL: &cleanupURL,
	}
}

func (service *Service) tyranoScriptCheckpointConfig(
	ctx context.Context,
	launchID string,
	saveStateID sql.NullString,
) (ButterscotchCheckpoint, bool, error) {
	if !saveStateID.Valid {
		return ButterscotchCheckpoint{}, false, nil
	}
	var payloadKind string
	err := service.database.QueryRowContext(ctx, `
SELECT save.payload_kind FROM launch_sessions launch
JOIN save_states save ON save.id=launch.save_state_id
JOIN save_state_runtime_compatibility compatibility ON compatibility.save_state_id=save.id
WHERE launch.id=? AND save.id=? AND save.deleted_at_ms IS NULL
 AND save.game_content_revision_id=launch.game_content_revision_id
 AND save.game_variant_revision_id=launch.game_variant_revision_id
 AND compatibility.status='AVAILABLE'
`, launchID, saveStateID.String).Scan(&payloadKind)
	if errors.Is(err, sql.ErrNoRows) || payloadKind != "RUNTIME_STATE" {
		return ButterscotchCheckpoint{}, false, ErrCredential
	}
	if err != nil {
		return ButterscotchCheckpoint{}, false, fmt.Errorf("load TyranoScript checkpoint: %w", err)
	}
	return ButterscotchCheckpoint{
		PayloadKind: payloadKind, PayloadURL: "/runtime/launches/" + launchID + "/state",
	}, true, nil
}

func parseTyranoScriptCompatibility(raw string) (tyranoScriptCompatibility, error) {
	var value tyranoScriptCompatibility
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return tyranoScriptCompatibility{}, ErrCredential
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF ||
		value.AdapterABI != "tyranoscript-snapshot-v1" || value.BridgePath != "tyranoscript-bridge.js" ||
		value.GameCompatibilityLine != "tyranoscript-v1" || value.SaveABI != "tyranoscript-snapshot-v1" ||
		len(value.ReadableSaveABIs) != 1 || !slices.Contains(value.ReadableSaveABIs, value.SaveABI) {
		return tyranoScriptCompatibility{}, ErrCredential
	}
	return value, nil
}

func (service *Service) tyranoScriptBridgeAvailable(runtimeVersion, bridgePath string) bool {
	_, file, exists := service.dependencies.RetromRuntimeFile(runtimeVersion, bridgePath)
	return exists && file.Role == "adapter_bridge"
}
