package validation

import "errors"

var (
	ErrInvalidState    = errors.New("RPG_RUNTIME_INVALID_STATE")
	ErrProtocol        = errors.New("RPG_RUNTIME_PROTOCOL_VIOLATION")
	ErrDecisionInvalid = errors.New("RPG_RUNTIME_VALIDATION_DECISION_INVALID")
)

type State string

const (
	StateCreated          State = "CREATED"
	StateStarting         State = "STARTING"
	StateRunning          State = "RUNNING"
	StateCheckpointed     State = "CHECKPOINTED"
	StateRestored         State = "RESTORED"
	StateAwaitingDecision State = "AWAITING_DECISION"
	StatePassed           State = "PASSED"
	StateFailed           State = "FAILED"
	StateExpired          State = "EXPIRED"
)

type Gate string

const (
	GateRuntimeReady          Gate = "RUNTIME_READY"
	GateEngineProfile         Gate = "ENGINE_PROFILE"
	GateFrames300             Gate = "FRAMES_300"
	GateInput                 Gate = "INPUT"
	GateAudio                 Gate = "AUDIO"
	GateInitialPosition       Gate = "INITIAL_POSITION_RECORDED"
	GateSavePointRecorded     Gate = "SAVE_POINT_RECORDED"
	GateCheckpointCreated     Gate = "CHECKPOINT_CREATED"
	GatePostSaveStateDiverged Gate = "POST_SAVE_STATE_DIVERGED"
	GateOriginalLaunchEnded   Gate = "ORIGINAL_LAUNCH_ENDED"
	GateRestoreStarted        Gate = "RESTORE_STARTED"
	GateRestorePosition       Gate = "RESTORE_POSITION_VERIFIED"
	GateRestoreScreenshot     Gate = "RESTORE_SCREENSHOT"
	GateRestoreInput          Gate = "RESTORE_INPUT"
)

type Phase string

const (
	PhaseBegin Phase = "BEGIN"
	PhasePass  Phase = "PASS"
	PhaseFail  Phase = "FAIL"
)

type Position struct {
	MapID        int64 `json:"mapId"`
	PlayerX      int64 `json:"playerX"`
	PlayerY      int64 `json:"playerY"`
	FixtureState int64 `json:"fixtureState"`
}

type Event struct {
	Sequence     int64
	EventID      string
	LaunchID     string
	Gate         Gate
	Phase        Phase
	ObservedAtMS int64
	Position     *Position
}

type GateResult struct {
	BeginAtMS    int64
	TerminalAtMS int64
	Phase        Phase
}

type ApplyContext struct {
	RestoreScreenshotPersisted bool
}

type Machine struct {
	ValidationID    string
	LaunchID        string
	RestoreLaunchID string
	State           State
	LastSequence    int64
	FailureCode     string
	Gates           map[Gate]GateResult
	Initial         *Position
	SavePoint       *Position
	Diverged        *Position
	Restored        *Position
	RestoreInput    *Position
	events          map[string]Event
}
