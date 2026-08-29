package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
)

var (
	ErrBlocked          = errors.New("LAUNCH_BLOCKED")
	ErrCredential       = errors.New("LAUNCH_CREDENTIAL_INVALID")
	ErrDOSEntryMissing  = errors.New("LAUNCH_DOS_ENTRY_MISSING")
	ErrDOSEntryUnsafe   = errors.New("LAUNCH_DOS_ENTRY_UNSAFE")
	ErrSaveIncompatible = errors.New("LAUNCH_SAVE_INCOMPATIBLE")
)

const reviewScreenshotOverrideCode = "REVIEW_SCREENSHOT_OVERRIDE"

type Capabilities struct {
	SecureContext       bool `json:"secureContext"`
	CrossOriginIsolated bool `json:"crossOriginIsolated"`
	SharedArrayBuffer   bool `json:"sharedArrayBuffer"`
}

type CreateRequest struct {
	GameID             string       `json:"gameId"`
	CoreID             *string      `json:"coreId"`
	SaveStateID        *string      `json:"saveStateId"`
	DOSEntry           *string      `json:"dosEntry"`
	ReturnTo           string       `json:"returnTo"`
	ClientCapabilities Capabilities `json:"clientCapabilities"`
}

type Created struct {
	Status               string   `json:"status,omitempty"`
	JobID                string   `json:"jobId,omitempty"`
	RetryAfterMS         int64    `json:"retryAfterMs,omitempty"`
	LaunchID             string   `json:"launchId"`
	PlayURL              string   `json:"playUrl"`
	Warnings             []string `json:"warnings"`
	BootstrapExpiresAtMS int64    `json:"bootstrapExpiresAtMs"`
	HardExpiresAtMS      int64    `json:"hardExpiresAtMs"`
	Capability           string   `json:"-"`
	Existing             bool     `json:"-"`
}

type NetplayCreateRequest struct {
	RoomID                  string
	SessionID               string
	ProfileID               string
	PlayerNo                int
	GameID                  string
	GameVariantRevisionID   string
	CoreArtifactID          string
	ReturnTo                string
	ClientCapabilities      Capabilities
	CredentialGeneration    int64
	NetplayCredentialSHA256 []byte
}

type artifactCompatibility struct {
	SchemaVersion             int                          `json:"schemaVersion"`
	RuntimeCoreID             string                       `json:"runtimeCoreId"`
	AdapterABI                string                       `json:"adapterAbi"`
	RequestedArtifactBasename string                       `json:"requestedArtifactBasename"`
	CanvasResizePolicy        string                       `json:"canvasResizePolicy"`
	DefaultOptions            map[string]string            `json:"defaultOptions"`
	InputMode                 string                       `json:"inputMode"`
	StartupActions            []dependencies.StartupAction `json:"startupActions"`
	SupportedContentKinds     []string                     `json:"supportedContentKinds,omitempty"`
	MultiDisc                 *struct {
		MaxDiscs      int    `json:"maxDiscs"`
		MaxTotalBytes int64  `json:"maxTotalBytes"`
		Delivery      string `json:"delivery"`
	} `json:"multiDisc,omitempty"`
}

type Service struct {
	database                 *sql.DB
	dependencies             *dependencies.Set
	credentials              *retromruntime.Credentials
	blobs                    *blobstore.Store
	rpgRuntimeOriginTemplate string
	now                      func() time.Time
}

func New(
	database *sql.DB,
	dependencySet *dependencies.Set,
	credentials *retromruntime.Credentials,
	now func() time.Time,
) *Service {
	return &Service{database: database, dependencies: dependencySet, credentials: credentials, now: now}
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func (service *Service) WithRPGRuntimeOriginTemplate(template string) *Service {
	service.rpgRuntimeOriginTemplate = template
	return service
}

func (service *Service) SaveAccess(ctx context.Context, launchID, capability string) (string, error) {
	var credentialHash []byte
	var state, access string
	var hardExpires int64
	if err := service.database.QueryRowContext(ctx, `
SELECT credential_sha256,state,hard_expires_at_ms,save_access FROM launch_sessions WHERE id=?
`, launchID).Scan(&credentialHash, &state, &hardExpires, &access); err != nil ||
		!retromruntime.MatchesCapability(capability, credentialHash) || hardExpires <= service.now().UnixMilli() ||
		state == "FINISHED" || state == "EXPIRED" || state == "REVOKED" {
		return "", ErrCredential
	}
	return access, nil
}

// CreateNetplay creates the participant-owned launch from a server-locked
// session snapshot. It deliberately bypasses save-state selection while
// retaining the normal content/BIOS locking path.
func (service *Service) CreateNetplay(ctx context.Context, request NetplayCreateRequest) (Created, error) {
	if !validNetplayCreateRequest(request) {
		return Created{}, ErrBlocked
	}
	contentPlan, err := service.prepareNetplayLaunch(ctx, request)
	if err != nil {
		return Created{}, err
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("launch/netplay: %w", err)
	}
	defer cleanup.Rollback(transaction)
	binding, err := loadNetplayParticipantBinding(ctx, transaction, request)
	if err != nil {
		return Created{}, err
	}
	if binding.existingLaunch.Valid {
		return service.existingNetplayLaunch(ctx, transaction, binding.existingLaunch.String, now)
	}
	if binding.participantState != "LOCKED" || binding.generation != 0 || request.CredentialGeneration != 1 {
		return Created{}, ErrBlocked
	}
	return service.insertNetplayLaunch(ctx, transaction, request, contentPlan, now)
}

func validNetplayCreateRequest(request NetplayCreateRequest) bool {
	return request.ProfileID != "" && request.PlayerNo >= 1 && request.PlayerNo <= 4 &&
		request.CredentialGeneration >= 1 && len(request.NetplayCredentialSHA256) == 32 &&
		request.ReturnTo == "/netplay/rooms/"+request.RoomID
}

func (service *Service) prepareNetplayLaunch(
	ctx context.Context, request NetplayCreateRequest,
) (launchContentPlan, error) {
	var selectedCore, emulatorVersion, contentKind string
	var requiresThreads int
	err := service.database.QueryRowContext(ctx, `
SELECT artifact.core_id,artifact.runtime_version,artifact.requires_threads,content.content_kind
FROM netplay_sessions session
JOIN core_artifacts artifact ON artifact.id=session.core_artifact_id
JOIN game_variant_revisions revision ON revision.id=session.game_variant_revision_id
JOIN game_content_revisions content ON content.id=revision.game_content_revision_id
WHERE session.id=? AND session.room_id=? AND session.game_id=?
  AND session.game_variant_revision_id=? AND session.core_artifact_id=?
  AND artifact.runtime_family='EMULATORJS' AND artifact.available_for_launch=1
  AND session.state NOT IN ('FINISHED','FAILED')
	`, request.SessionID, request.RoomID, request.GameID, request.GameVariantRevisionID, request.CoreArtifactID).
		Scan(&selectedCore, &emulatorVersion, &requiresThreads, &contentKind)
	if err != nil || contentKind != "SINGLE_FILE" || service.dependencies.Versions[emulatorVersion] == nil {
		return launchContentPlan{}, ErrBlocked
	}
	if requiresThreads == 1 && (!request.ClientCapabilities.SecureContext ||
		!request.ClientCapabilities.CrossOriginIsolated || !request.ClientCapabilities.SharedArrayBuffer) {
		return launchContentPlan{}, ErrBlocked
	}
	compatibility, err := service.loadArtifactCompatibility(ctx, request.CoreArtifactID)
	if err != nil {
		return launchContentPlan{}, ErrBlocked
	}
	contentPlan, err := service.buildLaunchContentPlan(ctx, request.GameVariantRevisionID, selectedCore, compatibility)
	if err != nil || contentPlan.ContentKind != "SINGLE_FILE" || len(contentPlan.Discs) != 0 {
		return launchContentPlan{}, ErrBlocked
	}
	primary, ok := contentPlan.singleFile()
	if !ok {
		return launchContentPlan{}, ErrBlocked
	}
	if err := service.validateLaunchLogicalNames(
		ctx, request.GameVariantRevisionID, primary.LogicalName,
	); err != nil {
		return launchContentPlan{}, err
	}
	return contentPlan, nil
}

type netplayParticipantBinding struct {
	existingLaunch   sql.NullString
	participantState string
	generation       int64
}

func loadNetplayParticipantBinding(
	ctx context.Context, transaction *sql.Tx, request NetplayCreateRequest,
) (netplayParticipantBinding, error) {
	var result netplayParticipantBinding
	if err := transaction.QueryRowContext(ctx, `
SELECT state,credential_generation,launch_session_id
FROM netplay_session_participants
WHERE netplay_session_id=? AND profile_id=? AND player_no=?
`, request.SessionID, request.ProfileID, request.PlayerNo).
		Scan(&result.participantState, &result.generation, &result.existingLaunch); err != nil {
		return netplayParticipantBinding{}, ErrBlocked
	}
	return result, nil
}

func (service *Service) existingNetplayLaunch(
	ctx context.Context, transaction *sql.Tx, launchIDValue string, now int64,
) (Created, error) {
	var state string
	var bootstrapExpires, hardExpires int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,bootstrap_expires_at_ms,hard_expires_at_ms FROM launch_sessions WHERE id=?
`, launchIDValue).Scan(&state, &bootstrapExpires, &hardExpires); err != nil ||
		(state != "CREATED" && state != "ACTIVE") || hardExpires <= now {
		return Created{}, ErrBlocked
	}
	launchID, err := uuid.Parse(launchIDValue)
	if err != nil {
		return Created{}, ErrBlocked
	}
	capability := service.credentials.Capability(launchID)
	return Created{
		LaunchID: launchIDValue, PlayURL: "/play/" + launchIDValue, Warnings: []string{},
		BootstrapExpiresAtMS: bootstrapExpires, HardExpiresAtMS: hardExpires,
		Capability: retromruntime.EncodeCapability(capability), Existing: true,
	}, nil
}

func (service *Service) insertNetplayLaunch(
	ctx context.Context,
	transaction *sql.Tx,
	request NetplayCreateRequest,
	contentPlan launchContentPlan,
	now int64,
) (Created, error) {
	launchID, err := uuid.NewV7()
	if err != nil {
		return Created{}, fmt.Errorf("launch/netplay: %w", err)
	}
	capability := service.credentials.Capability(launchID)
	capabilityHash := retromruntime.HashCapability(capability)
	bootstrapExpires := now + int64(5*time.Minute/time.Millisecond)
	hardExpires := now + int64(8*time.Hour/time.Millisecond)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_sessions(
  id,profile_id,purpose,game_id,game_content_revision_id,game_variant_revision_id,
  core_artifact_id,route_key,save_state_id,dos_entry_path,
  initial_disc_index,return_to,credential_sha256,state,
  bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,
  netplay_session_id,netplay_player_no,save_access
) SELECT ?,?,'PRODUCT',?,revision.game_content_revision_id,revision.id,
artifact.id,revision.route_key,NULL,NULL,0,?,?,'CREATED',?,?,?,?,?,?,'NETPLAY_DISABLED'
FROM game_variant_revisions revision
JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
WHERE revision.id=? AND artifact.id=? AND artifact.runtime_family='EMULATORJS'
  AND artifact.available_for_launch=1
`, launchID.String(), request.ProfileID, request.GameID,
		request.ReturnTo, capabilityHash[:], bootstrapExpires, hardExpires, now, now,
		request.SessionID, request.PlayerNo, request.GameVariantRevisionID, request.CoreArtifactID); err != nil {
		return Created{}, fmt.Errorf("create netplay launch: %w", err)
	}
	for _, file := range contentPlan.Files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
VALUES(?,?,?,?,?)
`, launchID.String(), file.LogicalName, file.BlobID, file.Format, now); err != nil {
			return Created{}, fmt.Errorf("lock netplay launch content: %w", err)
		}
	}
	if err := service.lockExternalBIOS(
		ctx, transaction, launchID.String(), request.GameVariantRevisionID, now, false,
	); err != nil {
		return Created{}, err
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants
SET launch_session_id=?,credential_sha256=?,credential_generation=?,state='LAUNCH_READY',
  version=version+1,updated_at_ms=?
WHERE netplay_session_id=? AND profile_id=? AND player_no=? AND state='LOCKED' AND credential_generation=0
`, launchID.String(), request.NetplayCredentialSHA256, request.CredentialGeneration, now,
		request.SessionID, request.ProfileID, request.PlayerNo)
	if err != nil {
		return Created{}, fmt.Errorf("bind netplay launch: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Created{}, ErrBlocked
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("commit netplay launch: %w", err)
	}
	return Created{
		LaunchID: launchID.String(), PlayURL: "/play/" + launchID.String(), Warnings: []string{},
		BootstrapExpiresAtMS: bootstrapExpires, HardExpiresAtMS: hardExpires,
		Capability: retromruntime.EncodeCapability(capability),
	}, nil
}
