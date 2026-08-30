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

	"retrom/internal/butterscotch/detector"
	retromruntime "retrom/internal/runtime"
)

const butterscotchProjectFormat = "BUTTERSCOTCH_PROJECT_V1"

type ButterscotchAdapterConfig struct {
	AdapterKind     string `json:"adapterKind"`
	AdapterID       string `json:"adapterId"`
	RuntimeBaseURL  string `json:"runtimeBaseUrl"`
	ProjectIndexURL string `json:"projectIndexUrl"`
}

type ButterscotchConfig struct {
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
	Adapter         ButterscotchAdapterConfig `json:"adapter"`
	Checkpoint      *ButterscotchCheckpoint   `json:"checkpoint"`
	ReviewPreview   *ReviewPreviewConfig      `json:"reviewPreview,omitempty"`
}

type ButterscotchCheckpoint struct {
	PayloadKind string `json:"payloadKind"`
	PayloadURL  string `json:"payloadUrl"`
}

type butterscotchCompatibility struct {
	AdapterABI            string   `json:"adapterAbi"`
	GameCompatibilityLine string   `json:"gameCompatibilityLine"`
	JSPath                string   `json:"jsPath"`
	ReadableSaveABIs      []string `json:"readableSaveAbis"`
	SaveABI               string   `json:"saveAbi"`
	WasmPath              string   `json:"wasmPath"`
	WorkerPath            string   `json:"workerPath"`
}

func (service *Service) buildButterscotchReviewConfig(
	ctx context.Context,
	previewID string,
	capability string,
	source reviewPreviewConfigSource,
) (Config, error) {
	if _, err := detector.ParseSnapshot(source.DependencyJSON); err != nil {
		return Config{}, ErrCredential
	}
	compatibility, err := parseButterscotchCompatibility(source.CompatibilityJSON)
	if err != nil || source.RuntimeFamily != "BUTTERSCOTCH" || source.AdapterKind != "BUTTERSCOTCH_WEB" ||
		source.AdapterID != "butterscotch-web" || source.CoreID != "butterscotch" ||
		source.ContentFormat != butterscotchProjectFormat || source.RelativePath != compatibility.JSPath ||
		!service.butterscotchRuntimeFilesAvailable(source.RuntimeVersion, compatibility) {
		return Config{}, ErrCredential
	}
	projectRoot, err := service.ProjectContentRoot(ctx, previewID, capability)
	if err != nil {
		return Config{}, ErrCredential
	}
	contentDigest, err := service.ProjectContentIdentity(ctx, previewID, capability)
	if err != nil {
		return Config{}, ErrCredential
	}
	configuration := ButterscotchConfig{
		RuntimeFamily: "BUTTERSCOTCH", ProtocolVersion: 1, Mode: "single", Purpose: "REVIEW_PREVIEW",
		LaunchID: previewID, SessionID: previewID, CoreID: source.CoreID, CoreName: source.CoreName,
		GameTitle: source.Title, PlatformName: source.PlatformName, RuntimeVersion: source.RuntimeVersion,
		ArtifactID: source.ArtifactID, ContentDigest: contentDigest, ReturnTo: "/admin/reviews/" + source.ItemID,
		Warnings: []string{"REVIEW_PREVIEW_BEST_EFFORT"},
		Adapter: ButterscotchAdapterConfig{
			AdapterKind: source.AdapterKind, AdapterID: source.AdapterID,
			RuntimeBaseURL:  "/runtime/retrom-runtime/" + source.RuntimeVersion + "/",
			ProjectIndexURL: projectRoot + "index.json",
		},
		ReviewPreview: &ReviewPreviewConfig{
			ImportItemID: source.ItemID, CaptureAllowed: source.CaptureAllowed == 1,
			CaptureAfterMS: reviewCaptureAfterMS,
		},
	}
	return Config{RuntimeFamily: "BUTTERSCOTCH", Butterscotch: &configuration}, nil
}

func (service *Service) butterscotchProductConfig(
	ctx context.Context,
	launchID, capability string,
) (ButterscotchConfig, error) {
	source, err := service.loadProjectProductConfigSource(ctx, launchID, "BUTTERSCOTCH")
	if err != nil || !retromruntime.MatchesCapability(capability, source.credentialHash) ||
		!validConfigLifetime(source.state, source.bootstrapExpires, source.hardExpires, source.idleExpires,
			service.now().UnixMilli()) {
		return ButterscotchConfig{}, ErrCredential
	}
	if _, err := detector.ParseSnapshot(source.dependencyJSON); err != nil ||
		source.adapterKind != "BUTTERSCOTCH_WEB" || source.adapterID != "butterscotch-web" ||
		source.coreID != "butterscotch" {
		return ButterscotchConfig{}, ErrCredential
	}
	compatibility, err := parseButterscotchCompatibility(source.compatibilityJSON)
	if err != nil || !service.butterscotchRuntimeFilesAvailable(source.runtimeVersion, compatibility) {
		return ButterscotchConfig{}, ErrBlocked
	}
	var checkpoint *ButterscotchCheckpoint
	if source.saveStateID.Valid {
		checkpoint, err = service.butterscotchCheckpointConfig(ctx, launchID, source.saveStateID.String)
		if err != nil {
			return ButterscotchConfig{}, err
		}
	}
	projectRoot, err := service.ProjectContentRoot(ctx, launchID, capability)
	if err != nil {
		return ButterscotchConfig{}, ErrCredential
	}
	contentDigest, err := service.ProjectContentIdentity(ctx, launchID, capability)
	if err != nil {
		return ButterscotchConfig{}, ErrCredential
	}
	if err := service.activateRuntimeLaunch(ctx, launchID, source.state); err != nil {
		return ButterscotchConfig{}, err
	}
	return ButterscotchConfig{
		RuntimeFamily: "BUTTERSCOTCH", ProtocolVersion: 1, Mode: "single", Purpose: "PRODUCT",
		LaunchID: launchID, SessionID: launchID, CoreID: source.coreID, CoreName: source.coreName,
		GameTitle: source.gameTitle, PlatformName: source.platform, RuntimeVersion: source.runtimeVersion,
		ArtifactID: source.artifactID, ContentDigest: contentDigest, ReturnTo: source.returnTo, Warnings: []string{},
		Adapter: ButterscotchAdapterConfig{
			AdapterKind: source.adapterKind, AdapterID: source.adapterID,
			RuntimeBaseURL:  "/runtime/retrom-runtime/" + source.runtimeVersion + "/",
			ProjectIndexURL: projectRoot + "index.json",
		},
		Checkpoint: checkpoint,
	}, nil
}

func (service *Service) butterscotchCheckpointConfig(
	ctx context.Context,
	launchID, saveStateID string,
) (*ButterscotchCheckpoint, error) {
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
		return nil, fmt.Errorf("load Butterscotch product checkpoint: %w", err)
	}
	return &ButterscotchCheckpoint{
		PayloadKind: payloadKind, PayloadURL: "/runtime/launches/" + launchID + "/state",
	}, nil
}

func parseButterscotchCompatibility(raw string) (butterscotchCompatibility, error) {
	var value butterscotchCompatibility
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return butterscotchCompatibility{}, ErrCredential
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF ||
		value.AdapterABI != "butterscotch-checkpoint-v2" ||
		value.GameCompatibilityLine != "butterscotch-gamemaker-v1" ||
		value.SaveABI != "butterscotch-checkpoint-v2" || len(value.ReadableSaveABIs) != 1 ||
		!slices.Contains(value.ReadableSaveABIs, value.SaveABI) || value.JSPath != "butterscotch.mjs" ||
		value.WasmPath != "butterscotch.wasm" || value.WorkerPath != "butterscotch-worker.mjs" {
		return butterscotchCompatibility{}, ErrCredential
	}
	return value, nil
}

func (service *Service) butterscotchRuntimeFilesAvailable(
	runtimeVersion string,
	compatibility butterscotchCompatibility,
) bool {
	for _, file := range []string{compatibility.JSPath, compatibility.WasmPath, compatibility.WorkerPath} {
		if _, _, exists := service.dependencies.RetromRuntimeFile(runtimeVersion, file); !exists {
			return false
		}
	}
	return true
}
