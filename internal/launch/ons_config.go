package launch

import (
	"encoding/json"
	"io"
	"strings"

	"retrom/internal/ons/detector"
)

const onsProjectFormat = "ONS_PROJECT_V1"

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
	AdapterABI     string `json:"adapterAbi"`
	CheckpointSlot int    `json:"checkpointSlot"`
	JSPath         string `json:"jsPath"`
	WasmPath       string `json:"wasmPath"`
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

func parseONSCompatibility(raw string) (onsCompatibility, error) {
	var value onsCompatibility
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return onsCompatibility{}, ErrCredential
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || value.AdapterABI != "ons-save" ||
		value.CheckpointSlot != 999 || value.JSPath != "onsyuri.js" || value.WasmPath != "onsyuri.wasm" {
		return onsCompatibility{}, ErrCredential
	}
	return value, nil
}
