package main

import "time"

const (
	caseID           = "ACC-RPG-012"
	stateVersion     = 1
	phaseOldSelected = "OLD_SELECTED"
	phaseNewSelected = "NEW_SELECTED"
	phaseDriftSeeded = "DRIFT_SEEDED"
)

type artifactBinding struct {
	ID                     string `json:"id"`
	CoreID                 string `json:"coreId"`
	Generation             string `json:"generation"`
	RouteKey               string `json:"routeKey"`
	ArtifactSetSHA256      string `json:"artifactSetSha256"`
	AdapterID              string `json:"adapterId"`
	AdapterABI             string `json:"adapterAbi"`
	ManifestSHA256         string `json:"manifestSha256"`
	SelectedForNewBindings bool   `json:"selectedForNewBindings"`
	AvailableForLaunch     bool   `json:"availableForLaunch"`
}

type packBinding struct {
	Slot           int    `json:"slot"`
	DeclaredName   string `json:"declaredName"`
	DefinitionID   string `json:"definitionId"`
	InstallationID string `json:"installationId"`
	FilesDigest    string `json:"filesDigest"`
}

type checkpointBinding struct {
	GameID                   string        `json:"gameId"`
	SaveStateID              string        `json:"saveStateId"`
	ContentRevisionID        string        `json:"contentRevisionId"`
	VariantRevisionID        string        `json:"variantRevisionId"`
	ArtifactID               string        `json:"artifactId"`
	RouteKey                 string        `json:"routeKey"`
	ProjectFingerprint       string        `json:"projectFingerprint"`
	AdapterABI               string        `json:"adapterAbi"`
	DependencySnapshotSHA256 string        `json:"dependencySnapshotSha256"`
	RuntimePacks             []packBinding `json:"runtimePacks"`
}

type variantBinding struct {
	GameID                   string        `json:"gameId"`
	ContentRevisionID        string        `json:"contentRevisionId"`
	VariantRevisionID        string        `json:"variantRevisionId"`
	ArtifactID               string        `json:"artifactId"`
	RouteKey                 string        `json:"routeKey"`
	ProjectFingerprint       string        `json:"projectFingerprint"`
	AdapterABI               string        `json:"adapterAbi"`
	DependencySnapshotSHA256 string        `json:"dependencySnapshotSha256"`
	RuntimePacks             []packBinding `json:"runtimePacks"`
}

type driftBinding struct {
	Content    string `json:"content"`
	Artifact   string `json:"artifact"`
	Pack       string `json:"pack"`
	AdapterABI string `json:"adapterAbi"`
}

type seedState struct {
	SchemaVersion      int                `json:"schemaVersion"`
	CaseID             string             `json:"caseId"`
	Phase              string             `json:"phase"`
	DatabasePathSHA256 string             `json:"databasePathSha256"`
	OldArtifact        artifactBinding    `json:"oldArtifact"`
	NewArtifact        artifactBinding    `json:"newArtifact"`
	OldCheckpoint      *checkpointBinding `json:"oldCheckpoint"`
	NewVariant         *variantBinding    `json:"newVariant"`
	DriftSaveStateIDs  *driftBinding      `json:"driftSaveStateIds"`
	UpdatedAtMS        int64              `json:"updatedAtMs"`
}

type clock func() time.Time
