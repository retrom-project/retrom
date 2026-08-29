package runtimevalidation

import "encoding/json"

const (
	GateNotStarted = "NOT_STARTED"
	GateInProgress = "IN_PROGRESS"
	GatePassed     = "PASSED"
	GateFailed     = "FAILED"
)

type MachineGate struct {
	Gate          string          `json:"gate"`
	Status        string          `json:"status"`
	BegunAtMS     *int64          `json:"begunAtMs"`
	CompletedAtMS *int64          `json:"completedAtMs"`
	Evidence      json.RawMessage `json:"evidence"`
	FailureCode   *string         `json:"failureCode"`
}

type GateRequest struct {
	Sequence     int64           `json:"sequence"`
	EventID      string          `json:"eventId"`
	Gate         string          `json:"gate"`
	Phase        string          `json:"phase"`
	ObservedAtMS int64           `json:"observedAtMs"`
	Evidence     json.RawMessage `json:"evidence"`
}

type GateAccepted struct {
	Sequence         int64  `json:"sequence"`
	EventID          string `json:"eventId"`
	ValidationState  string `json:"validationState"`
	IdempotentReplay bool   `json:"idempotentReplay"`
}

type RouteEvidence struct {
	EffectiveSourceSnapshotID string  `json:"effectiveSourceSnapshotId"`
	CoreID                    string  `json:"coreId"`
	Generation                string  `json:"generation"`
	EvidenceGeneration        *string `json:"evidenceGeneration"`
	EvidenceConfidence        string  `json:"evidenceConfidence"`
	RouteKey                  string  `json:"routeKey"`
	ArtifactID                string  `json:"artifactId"`
	ArtifactSetSHA256         string  `json:"artifactSetSha256"`
	AdapterID                 string  `json:"adapterId"`
	AdapterABI                string  `json:"adapterAbi"`
	DependencySnapshotSHA256  string  `json:"dependencySnapshotSha256"`
	ProjectFingerprint        string  `json:"projectFingerprint"`
}

type Position struct {
	MapID        int64 `json:"mapId"`
	PlayerX      int64 `json:"playerX"`
	PlayerY      int64 `json:"playerY"`
	FixtureState int64 `json:"fixtureState"`
}

type CheckpointRoundTrip struct {
	Created              bool      `json:"created"`
	PayloadKind          *string   `json:"payloadKind"`
	ResumeSlot           *int64    `json:"resumeSlot"`
	SizeBytes            *int64    `json:"sizeBytes"`
	SHA256               *string   `json:"sha256"`
	OriginalLaunchID     *string   `json:"originalLaunchId"`
	InitialPosition      *Position `json:"initialPosition"`
	SavedPosition        *Position `json:"savedPosition"`
	DivergedPosition     *Position `json:"divergedPosition"`
	OriginalLaunchEnded  bool      `json:"originalLaunchEnded"`
	RestoreLaunchID      *string   `json:"restoreLaunchId"`
	RestoreStarted       bool      `json:"restoreStarted"`
	RestoredPosition     *Position `json:"restoredPosition"`
	PositionVerified     bool      `json:"positionVerified"`
	ScreenshotURL        *string   `json:"screenshotUrl"`
	RestoreInputPosition *Position `json:"restoreInputPosition"`
	RestoreInputVerified bool      `json:"restoreInputVerified"`
}

type ReviewerDecision struct {
	Decision  string `json:"decision"`
	Note      string `json:"note"`
	DecidedAt int64  `json:"decidedAtMs"`
}

type View struct {
	ValidationID          string              `json:"validationId"`
	ImportItemID          string              `json:"importItemId"`
	ReviewVersionAtCreate int64               `json:"reviewVersionAtCreate"`
	RuntimeBindingVersion int64               `json:"runtimeBindingRevision"`
	LaunchID              *string             `json:"launchId"`
	RestoreLaunchID       *string             `json:"restoreLaunchId"`
	State                 string              `json:"state"`
	LastGateSequence      int64               `json:"lastGateSequence"`
	RouteEvidence         RouteEvidence       `json:"routeEvidence"`
	MachineGates          []MachineGate       `json:"machineGates"`
	CheckpointRoundTrip   CheckpointRoundTrip `json:"checkpointRoundTrip"`
	FailureCode           *string             `json:"failureCode"`
	Decision              *ReviewerDecision   `json:"decision"`
	CreatedAtMS           int64               `json:"createdAtMs"`
	UpdatedAtMS           int64               `json:"updatedAtMs"`
	ExpiresAtMS           int64               `json:"expiresAtMs"`
}

type Screenshot struct {
	ValidationID string
	ImportItemID string
	ArtifactID   string
	WidthPX      int64
	HeightPX     int64
	CapturedAtMS int64
}
