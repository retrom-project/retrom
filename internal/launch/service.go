package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	"retrom/internal/contentcapability"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/runtimecatalog"
	"retrom/internal/runtimelaunch"
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
	GameVariantID           string
	ProviderID              string
	TargetID                string
	BundleSHA256            string
	ReturnTo                string
	ClientCapabilities      Capabilities
	CredentialGeneration    int64
	NetplayCredentialSHA256 []byte
}

type Service struct {
	database                 *sql.DB
	dependencies             *dependencies.Set
	credentials              *retromruntime.Credentials
	blobs                    *blobstore.Store
	rpgRuntimeOriginTemplate string
	now                      func() time.Time
	runtimeCatalog           runtimecatalog.Catalog
	runtimeBuilder           *runtimelaunch.Builder
	publicOrigin             string
}

func (service *Service) WithRuntimeProvider(
	catalog runtimecatalog.Catalog,
	builder *runtimelaunch.Builder,
) *Service {
	service.runtimeCatalog = catalog
	service.runtimeBuilder = builder
	return service
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

func (service *Service) WithPublicOrigin(origin string) *Service {
	service.publicOrigin = origin
	return service
}

func (service *Service) netplaySocketURL(roomID string) (string, error) {
	origin, err := url.Parse(service.publicOrigin)
	if err != nil || origin.Host == "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" {
		return "", ErrCredential
	}
	switch origin.Scheme {
	case "http":
		origin.Scheme = "ws"
	case "https":
		origin.Scheme = "wss"
	default:
		return "", ErrCredential
	}
	origin.Path = "/runtime/netplay/rooms/" + roomID + "/socket"
	return origin.String(), nil
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
	preparation, err := service.prepareNetplayLaunch(ctx, request)
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
	return service.insertNetplayLaunch(ctx, transaction, request, preparation, now)
}

func validNetplayCreateRequest(request NetplayCreateRequest) bool {
	return request.ProfileID != "" && request.PlayerNo >= 1 && request.PlayerNo <= 4 &&
		request.ProviderID != "" && request.TargetID != "" && len(request.BundleSHA256) == 64 &&
		request.CredentialGeneration >= 1 && len(request.NetplayCredentialSHA256) == 32 &&
		request.ReturnTo == "/netplay/rooms/"+request.RoomID
}

type netplayLaunchPreparation struct {
	selection launchSelection
	content   launchContentPlan
}

func (service *Service) prepareNetplayLaunch(
	ctx context.Context, request NetplayCreateRequest,
) (netplayLaunchPreparation, error) {
	if service.runtimeBuilder == nil {
		return netplayLaunchPreparation{}, ErrBlocked
	}
	var selection launchSelection
	err := service.database.QueryRowContext(ctx, `
SELECT variant.id,variant.core_id,session.provider_id,session.target_id,
 session.bundle_sha256,game.id,game.content_kind,
 binding.delivery_profile,
 `+contentcapability.BindingPolicySQL+`,variant.dependency_snapshot_json,
 COALESCE((SELECT file.logical_name FROM game_files file
  WHERE file.game_id=game.id AND file.role='CONTENT'
  ORDER BY file.sort_order,file.logical_name LIMIT 1),''),variant.dat_version_id
FROM netplay_sessions session
JOIN game_variants variant ON variant.id=session.game_variant_id
JOIN games game ON game.id=session.game_id AND game.id=variant.game_id
JOIN runtime_targets target ON target.provider_id=session.provider_id AND target.target_id=session.target_id
JOIN runtime_providers provider ON provider.provider_id=session.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=session.provider_id AND binding.target_id=session.target_id
WHERE session.id=? AND session.room_id=? AND session.game_id=?
  AND session.game_variant_id=? AND session.provider_id=? AND session.target_id=?
	AND session.bundle_sha256=?
	AND variant.provider_id=session.provider_id AND variant.target_id=session.target_id
	AND variant.status='READY' AND binding.core_id=variant.core_id AND binding.launch_policy!='DISABLED'
  AND session.state NOT IN ('FINISHED','FAILED')
	`, request.SessionID, request.RoomID, request.GameID, request.GameVariantID,
		request.ProviderID, request.TargetID, request.BundleSHA256).
		Scan(
			&selection.variantID, &selection.selectedCore,
			&selection.providerID, &selection.targetID, &selection.bundleSHA256, &selection.gameID,
			&selection.contentKind, &selection.deliveryProfile, &selection.contentPolicy,
			&selection.dependencySnapshotJSON, &selection.contentLogicalName, &selection.datID,
		)
	if err != nil || selection.contentKind != "SINGLE_FILE" {
		return netplayLaunchPreparation{}, ErrBlocked
	}
	target, exists := service.runtimeBuilder.Target(selection.providerID, selection.targetID)
	if !exists || !target.Capabilities.NetplayPort || !validThreadCapabilities(
		target.Capabilities.RequiresThreads, request.ClientCapabilities,
	) {
		return netplayLaunchPreparation{}, ErrBlocked
	}
	contentPlan, err := service.buildProviderContentPlan(ctx, selection)
	if err != nil || contentPlan.ContentKind != "SINGLE_FILE" || len(contentPlan.Discs) != 0 {
		return netplayLaunchPreparation{}, ErrBlocked
	}
	if _, ok := contentPlan.singleFile(); !ok {
		return netplayLaunchPreparation{}, ErrBlocked
	}
	return netplayLaunchPreparation{selection: selection, content: contentPlan}, nil
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
	preparation netplayLaunchPreparation,
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
  id,profile_id,purpose,game_id,core_id,provider_id,target_id,bundle_sha256,
  content_kind,dependency_snapshot_json,compatibility_code,
  save_state_id,dos_entry_path,
  initial_disc_index,return_to,credential_sha256,state,
  bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,
  netplay_session_id,netplay_player_no,save_access
) SELECT ?,?,'PRODUCT',?,?,?,?,?,?,?,?,
NULL,NULL,0,?,?,'CREATED',?,?,?,?,?,?,'NETPLAY_DISABLED'
FROM game_variants variant
WHERE variant.id=? AND variant.game_id=? AND variant.provider_id=? AND variant.target_id=?
  AND variant.status='READY'
`, launchID.String(), request.ProfileID, request.GameID, preparation.selection.selectedCore,
		preparation.selection.providerID, preparation.selection.targetID,
		preparation.selection.bundleSHA256, preparation.selection.contentKind,
		preparation.selection.dependencySnapshotJSON, preparation.selection.compatibilityCode,
		request.ReturnTo, capabilityHash[:], bootstrapExpires, hardExpires, now, now,
		request.SessionID, request.PlayerNo, request.GameVariantID, request.GameID,
		request.ProviderID, request.TargetID); err != nil {
		return Created{}, fmt.Errorf("create netplay launch: %w", err)
	}
	for _, file := range preparation.content.Files {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
VALUES(?,?,?,?,?)
`, launchID.String(), file.LogicalName, file.BlobID, file.Format, now); err != nil {
			return Created{}, fmt.Errorf("lock netplay launch content: %w", err)
		}
	}
	if err := service.lockExternalBIOS(
		ctx, transaction, launchID.String(), request.GameVariantID, now, false,
	); err != nil {
		return Created{}, err
	}
	if err := service.lockVariantBundleFiles(
		ctx, transaction, launchID.String(), request.GameVariantID, now,
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
