package gamecontent

import "context"

const DeleteGameOperation = "deleteAdminGame"

var (
	ErrDeleteGameNotFound             = ErrAdminGameNotFound
	ErrDeleteGameVersionConflict      = ErrAdminGameVersionConflict
	ErrDeleteGameConfirmationMismatch = errDeleteGameConfirmationMismatch{}
	ErrDeleteGameImpactStale          = errDeleteGameImpactStale{}
)

type errDeleteGameConfirmationMismatch struct{}

func (errDeleteGameConfirmationMismatch) Error() string {
	return "GAME_DELETE_CONFIRMATION_MISMATCH"
}

type errDeleteGameImpactStale struct{}

func (errDeleteGameImpactStale) Error() string { return "GAME_DELETE_IMPACT_STALE" }

// DeleteGameReader is the transaction-bound read port for administrative game
// deletion. The impact is read in the same transaction as the eventual write.
type DeleteGameReader interface {
	LoadDeleteGameState(context.Context, string) (DeleteGameState, error)
	LoadDeleteGameReplay(context.Context, string, string, int64) (DeleteGameReplay, bool, error)
	DeleteGameImpact(context.Context, string) (DeleteGameImpact, error)
}

// DeleteGameWriter is the transaction-bound write port for administrative game
// deletion. Implementations own SQL, payload scheduling, runtime transitions,
// audit persistence, and durable replay records.
type DeleteGameWriter interface {
	ScheduleGameDeletion(context.Context, string, int64, int64) (string, error)
	TransitionDeletedGameRuntime(context.Context, string, int64) error
	RecordDeleteGameAudit(context.Context, DeleteGameAudit) error
	StoreDeleteGameReplay(context.Context, DeleteGameReplayWrite) error
}

type DeleteGameState struct {
	Title, Status, PayloadState string
	PayloadReleaseJobID         *string
	Version                     int64
}

type DeleteGameImpact struct {
	ImpactDigest       string
	RegisteredBytes    string
	ExclusiveBytes     string
	SharedBytes        string
	BlobCount          int64
	SaveStateCount     int64
	AssetCount         int64
	ContentFileCount   int64
	ActiveLaunchCount  int64
	ActiveNetplayCount int64
	ReviewEventCount   int64
	SourceKinds        []string
}

type DeleteGameReplay struct {
	RequestDigest string
	HTTPStatus    int
	HeadersJSON   string
	Body          []byte
}

type DeleteGameReplayWrite struct {
	PrincipalID, Key, RequestDigest string
	HTTPStatus                      int
	HeadersJSON                     string
	Body                            []byte
	CreatedAtMS, ExpiresAtMS        int64
}

type DeleteGameAudit struct {
	GameID        string
	Actor         AuditActor
	Before, After any
	NowMS         int64
}

type DeleteGameRequest struct {
	GameID, PrincipalID, Key, RequestDigest string
	ConfirmTitle, ImpactDigest              string
	ExpectedVersion                         int64
	Actor                                   AuditActor
	NowMS                                   int64
}

type DeleteGameResponse struct {
	GameID              string  `json:"gameId"`
	Status              string  `json:"status"`
	PayloadState        string  `json:"payloadState"`
	PayloadReleaseJobID *string `json:"payloadReleaseJobId"`
}

type DeleteGameResult struct {
	HTTPStatus           int
	ETag                 string
	Body                 []byte
	Replay               DeleteGameReplay
	Replayed             bool
	PayloadReleaseQueued bool
}
