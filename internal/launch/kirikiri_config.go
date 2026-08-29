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

	"retrom/internal/kirikiri/detector"
	retromruntime "retrom/internal/runtime"
)

const kirikiriProjectFormat = "KIRIKIRI_PROJECT_V1"

type KiriKiriAdapterConfig struct {
	AdapterKind     string  `json:"adapterKind"`
	AdapterID       string  `json:"adapterId"`
	RuntimeBaseURL  string  `json:"runtimeBaseUrl"`
	ProjectIndexURL string  `json:"projectIndexUrl"`
	StartupXP3Path  *string `json:"startupXp3Path"`
	CheckpointSlot  int     `json:"checkpointSlot"`
}

type KiriKiriConfig struct {
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
	Adapter         KiriKiriAdapterConfig `json:"adapter"`
	Checkpoint      *KiriKiriCheckpoint   `json:"checkpoint"`
	ReviewPreview   *ReviewPreviewConfig  `json:"reviewPreview,omitempty"`
}

type KiriKiriCheckpoint struct {
	PayloadKind string `json:"payloadKind"`
	PayloadURL  string `json:"payloadUrl"`
}

type kirikiriCompatibility struct {
	AdapterABI            string   `json:"adapterAbi"`
	AssetsPath            string   `json:"assetsPath"`
	CheckpointSlot        int      `json:"checkpointSlot"`
	GameCompatibilityLine string   `json:"gameCompatibilityLine"`
	JSPath                string   `json:"jsPath"`
	ReadableSaveABIs      []string `json:"readableSaveAbis"`
	SaveABI               string   `json:"saveAbi"`
	VLFSPath              string   `json:"vlfsPath"`
	WasmPath              string   `json:"wasmPath"`
}

func (service *Service) buildKiriKiriReviewConfig(
	previewID string,
	source reviewPreviewConfigSource,
) (Config, error) {
	profile, err := detector.ParseSnapshot(source.DependencyJSON)
	if err != nil {
		return Config{}, ErrCredential
	}
	compatibility, err := parseKiriKiriCompatibility(source.CompatibilityJSON)
	if err != nil || source.RuntimeFamily != "KIRIKIRI" || source.AdapterKind != "KIRIKIRI2_WEB" ||
		source.AdapterID != "kirikiri2-web" || source.CoreID != "kirikiri2" ||
		source.ContentFormat != kirikiriProjectFormat || source.RelativePath != compatibility.JSPath ||
		!service.kiriKiriRuntimeFilesAvailable(source.RuntimeVersion, compatibility) {
		return Config{}, ErrCredential
	}
	configuration := KiriKiriConfig{
		RuntimeFamily: "KIRIKIRI", ProtocolVersion: 1, Mode: "single", Purpose: "REVIEW_PREVIEW",
		LaunchID: previewID, SessionID: previewID, CoreID: source.CoreID, CoreName: source.CoreName,
		GameTitle: source.Title, PlatformName: source.PlatformName, RuntimeVersion: source.RuntimeVersion,
		ArtifactID: source.ArtifactID, ReturnTo: "/admin/reviews/" + source.ItemID,
		Warnings: []string{"REVIEW_PREVIEW_BEST_EFFORT"},
		Adapter: KiriKiriAdapterConfig{
			AdapterKind: source.AdapterKind, AdapterID: source.AdapterID,
			RuntimeBaseURL:  "/runtime/retrom-runtime/" + source.RuntimeVersion + "/",
			ProjectIndexURL: "/runtime/projects/" + previewID + "/index.json",
			StartupXP3Path:  profile.StartupXP3Path, CheckpointSlot: compatibility.CheckpointSlot,
		},
		ReviewPreview: &ReviewPreviewConfig{
			ImportItemID: source.ItemID, CaptureAllowed: source.CaptureAllowed == 1,
			CaptureAfterMS: reviewCaptureAfterMS,
		},
	}
	return Config{RuntimeFamily: "KIRIKIRI", KiriKiri: &configuration}, nil
}

func (service *Service) kiriKiriProductConfig(
	ctx context.Context,
	launchID, capability string,
) (KiriKiriConfig, error) {
	source, err := service.loadProjectProductConfigSource(ctx, launchID, "KIRIKIRI")
	if err != nil || !retromruntime.MatchesCapability(capability, source.credentialHash) ||
		!validConfigLifetime(
			source.state, source.bootstrapExpires, source.hardExpires, source.idleExpires,
			service.now().UnixMilli(),
		) {
		return KiriKiriConfig{}, ErrCredential
	}
	profile, err := detector.ParseSnapshot(source.dependencyJSON)
	if err != nil || source.adapterKind != "KIRIKIRI2_WEB" || source.adapterID != "kirikiri2-web" ||
		source.coreID != "kirikiri2" {
		return KiriKiriConfig{}, ErrCredential
	}
	compatibility, err := parseKiriKiriCompatibility(source.compatibilityJSON)
	if err != nil || !service.kiriKiriRuntimeFilesAvailable(source.runtimeVersion, compatibility) {
		return KiriKiriConfig{}, ErrBlocked
	}
	var checkpoint *KiriKiriCheckpoint
	if source.saveStateID.Valid {
		checkpoint, err = service.kiriKiriCheckpointConfig(ctx, launchID, source.saveStateID.String)
		if err != nil {
			return KiriKiriConfig{}, err
		}
	}
	if err := service.activateRuntimeLaunch(ctx, launchID, source.state); err != nil {
		return KiriKiriConfig{}, err
	}
	return KiriKiriConfig{
		RuntimeFamily: "KIRIKIRI", ProtocolVersion: 1, Mode: "single", Purpose: "PRODUCT",
		LaunchID: launchID, SessionID: launchID, CoreID: source.coreID, CoreName: source.coreName,
		GameTitle: source.gameTitle, PlatformName: source.platform, RuntimeVersion: source.runtimeVersion,
		ArtifactID: source.artifactID, ReturnTo: source.returnTo, Warnings: []string{},
		Adapter: KiriKiriAdapterConfig{
			AdapterKind: source.adapterKind, AdapterID: source.adapterID,
			RuntimeBaseURL:  "/runtime/retrom-runtime/" + source.runtimeVersion + "/",
			ProjectIndexURL: "/runtime/projects/" + launchID + "/index.json",
			StartupXP3Path:  profile.StartupXP3Path, CheckpointSlot: compatibility.CheckpointSlot,
		},
		Checkpoint: checkpoint,
	}, nil
}

func (service *Service) kiriKiriCheckpointConfig(
	ctx context.Context,
	launchID, saveStateID string,
) (*KiriKiriCheckpoint, error) {
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
	if errors.Is(err, sql.ErrNoRows) || payloadKind != "KIRIKIRI_SAVE_BUNDLE_V1" {
		return nil, ErrCredential
	}
	if err != nil {
		return nil, fmt.Errorf("load KiriKiri product checkpoint: %w", err)
	}
	return &KiriKiriCheckpoint{
		PayloadKind: payloadKind, PayloadURL: "/runtime/launches/" + launchID + "/state",
	}, nil
}

func parseKiriKiriCompatibility(raw string) (kirikiriCompatibility, error) {
	var value kirikiriCompatibility
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return kirikiriCompatibility{}, ErrCredential
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || value.AdapterABI != "kirikiri-kag-bookmark" ||
		value.CheckpointSlot != 1999 || value.GameCompatibilityLine != "kirikiri2-kag-v1" ||
		value.SaveABI != "kirikiri-kag-bookmark-v1" || len(value.ReadableSaveABIs) != 1 ||
		!slices.Contains(value.ReadableSaveABIs, value.SaveABI) || value.JSPath != "index.js" ||
		value.WasmPath != "index.wasm" || value.VLFSPath != "vlfs.js" || value.AssetsPath != "assets.zip" {
		return kirikiriCompatibility{}, ErrCredential
	}
	return value, nil
}

func (service *Service) kiriKiriRuntimeFilesAvailable(
	runtimeVersion string,
	compatibility kirikiriCompatibility,
) bool {
	for _, file := range []string{
		compatibility.JSPath, compatibility.WasmPath, compatibility.VLFSPath, compatibility.AssetsPath,
	} {
		if _, _, exists := service.dependencies.RetromRuntimeFile(runtimeVersion, file); !exists {
			return false
		}
	}
	return true
}
