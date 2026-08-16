package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/dependencies"
	retromruntime "retrom/internal/runtime"
)

var (
	ErrBlocked         = errors.New("LAUNCH_BLOCKED")
	ErrCredential      = errors.New("LAUNCH_CREDENTIAL_INVALID")
	ErrDOSEntryMissing = errors.New("LAUNCH_DOS_ENTRY_MISSING")
	ErrDOSEntryUnsafe  = errors.New("LAUNCH_DOS_ENTRY_UNSAFE")
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
	RequestedArtifactBasename string                       `json:"requestedArtifactBasename"`
	CanvasResizePolicy        string                       `json:"canvasResizePolicy"`
	DefaultOptions            map[string]string            `json:"defaultOptions"`
	PersistentSaveMode        string                       `json:"persistentSaveMode"`
	PersistentSaveKind        *string                      `json:"persistentSaveKind"`
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
	database     *sql.DB
	dependencies *dependencies.Set
	credentials  *retromruntime.Credentials
	blobs        *blobstore.Store
	now          func() time.Time
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
// session snapshot. It deliberately bypasses save-state and persistent-save
// selection while retaining the normal content/BIOS locking path.
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
SELECT artifact.core_id,artifact.emulatorjs_version,core.requires_threads,content.content_kind
FROM netplay_sessions session
JOIN core_artifacts artifact ON artifact.id=session.core_artifact_id
JOIN cores core ON core.id=artifact.core_id
JOIN game_variant_revisions revision ON revision.id=session.game_variant_revision_id
JOIN game_content_revisions content ON content.id=revision.game_content_revision_id
WHERE session.id=? AND session.room_id=? AND session.game_id=?
  AND session.game_variant_revision_id=? AND session.core_artifact_id=?
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
	if err := service.validateLaunchLogicalNames(
		ctx, request.GameVariantRevisionID, contentPlan.LogicalName,
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
  id,profile_id,game_id,game_variant_revision_id,core_artifact_id,save_state_id,dos_entry_path,
  persistent_save_base_revision_id,initial_disc_index,return_to,credential_sha256,state,
  bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,
  netplay_session_id,netplay_player_no,save_access
) VALUES(?,?,?,?,?,NULL,NULL,NULL,0,?,?,'CREATED',?,?,?,?,?,?,'NETPLAY_DISABLED')
`, launchID.String(), request.ProfileID, request.GameID, request.GameVariantRevisionID,
		request.CoreArtifactID, request.ReturnTo, capabilityHash[:], bootstrapExpires, hardExpires, now, now,
		request.SessionID, request.PlayerNo); err != nil {
		return Created{}, fmt.Errorf("create netplay launch: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
VALUES(?,?,?,?,?)
`, launchID.String(), contentPlan.LogicalName, contentPlan.BlobID, contentPlan.Format, now); err != nil {
		return Created{}, fmt.Errorf("lock netplay launch content: %w", err)
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

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Create(ctx context.Context, profileID string, request CreateRequest) (Created, error) {
	if profileID == "" || request.GameID == "" || !validReturnTo(request.ReturnTo, request.GameID) {
		return Created{}, ErrBlocked
	}
	coreID := ""
	if request.CoreID != nil {
		coreID = *request.CoreID
	}
	var variantRevisionID, artifactID, selectedCore, emulatorVersion string
	var validationInputDigest, contentRevisionID, contentLogicalName, contentKind, revisionCompatibilityCode string
	var revisionDATID sql.NullString
	var requiresThreads int
	var savedDOSEntry sql.NullString
	var savedDiscIndex sql.NullInt64
	if request.SaveStateID != nil {
		if request.DOSEntry != nil {
			return Created{}, ErrBlocked
		}
		err := service.database.QueryRowContext(ctx, `
SELECT s.game_variant_revision_id,
s.core_artifact_id,
a.core_id,
a.emulatorjs_version,
c.requires_threads,
s.dos_entry_path,
s.disc_index,
r.game_content_revision_id,
content.content_kind,
r.compatibility_code,
COALESCE((SELECT file.logical_name FROM game_content_files file
WHERE file.game_content_revision_id=r.game_content_revision_id
AND file.role IN ('CONTENT','DISC') ORDER BY CASE file.role WHEN 'CONTENT' THEN 0 ELSE 1 END,
file.sort_order,file.logical_name LIMIT 1),'')
FROM save_states s
JOIN games g ON g.id=s.game_id
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN game_variant_revisions r ON r.id=s.game_variant_revision_id
AND r.core_artifact_id=s.core_artifact_id
JOIN game_content_revisions content ON content.id=r.game_content_revision_id
JOIN core_artifacts a ON a.id=s.core_artifact_id
JOIN cores c ON c.id=a.core_id
WHERE s.id=?
AND s.game_id=?
AND s.profile_id=?
AND s.deleted_at_ms IS NULL
AND g.status='PUBLISHED'
AND pi.enabled=1
AND r.status='READY'
`, *request.SaveStateID, request.GameID, profileID).
			Scan(
				&variantRevisionID, &artifactID, &selectedCore, &emulatorVersion, &requiresThreads,
				&savedDOSEntry, &savedDiscIndex, &contentRevisionID, &contentKind,
				&revisionCompatibilityCode, &contentLogicalName,
			)
		if err != nil || request.CoreID != nil && coreID != selectedCore {
			return Created{}, ErrBlocked
		}
	} else {
		query := `
SELECT v.current_revision_id,
r.core_artifact_id,
a.core_id,
a.emulatorjs_version,
c.requires_threads,
r.validation_input_digest,
r.compatibility_code,
r.game_content_revision_id,
r.dat_version_id,
content_revision.content_kind,
COALESCE(content.logical_name,'')
FROM games g
JOIN platform_instances pi ON pi.id=g.platform_instance_id
JOIN game_variants v ON v.game_id=g.id
JOIN game_variant_revisions r ON r.id=v.current_revision_id
AND r.game_content_revision_id=g.current_content_revision_id
JOIN core_artifacts a ON a.id=r.core_artifact_id
AND a.enabled=1
JOIN cores c ON c.id=a.core_id
JOIN game_content_revisions content_revision ON content_revision.id=r.game_content_revision_id
LEFT JOIN game_content_files content ON content.game_content_revision_id=r.game_content_revision_id
AND content.role IN ('CONTENT','DISC')
WHERE g.id=?
AND g.status='PUBLISHED'
AND pi.enabled=1
AND r.status='READY'
AND v.core_id=CASE WHEN ?='' THEN pi.default_core_id ELSE ? END
ORDER BY CASE content.role WHEN 'CONTENT' THEN 0 ELSE 1 END,content.sort_order,content.logical_name
LIMIT 1
`
		if err := service.database.QueryRowContext(ctx, query, request.GameID, coreID, coreID).Scan(
			&variantRevisionID,
			&artifactID,
			&selectedCore,
			&emulatorVersion,
			&requiresThreads,
			&validationInputDigest,
			&revisionCompatibilityCode,
			&contentRevisionID,
			&revisionDATID,
			&contentKind,
			&contentLogicalName,
		); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return service.ensureVariant(ctx, profileID, request, coreID, true)
			}
			return Created{}, ErrBlocked
		}
		if revisionCompatibilityCode != reviewScreenshotOverrideCode {
			biosSnapshot, biosStatus, _, resolveErr := corevalidation.ResolveBIOS(
				ctx,
				service.database,
				artifactID,
				contentLogicalName,
			)
			if resolveErr != nil || biosStatus != "READY" {
				return Created{}, ErrBlocked
			}
			expectedDigest := ""
			var digestErr error
			if contentKind == corevalidation.MultiDiscContentKind {
				expectedDigest, digestErr = service.expectedMultiDiscDigest(
					ctx, variantRevisionID, contentRevisionID, artifactID, revisionDATID, biosSnapshot,
				)
			} else {
				expectedDigest, digestErr = corevalidation.ValidationInputDigest(
					artifactID,
					contentRevisionID,
					revisionDATID,
					biosSnapshot,
				)
			}
			if digestErr != nil {
				return Created{}, ErrBlocked
			}
			if validationInputDigest != expectedDigest {
				return service.ensureVariant(ctx, profileID, request, coreID, true)
			}
		}
	}
	if requiresThreads == 1 &&
		(!request.ClientCapabilities.SecureContext ||
			!request.ClientCapabilities.CrossOriginIsolated ||
			!request.ClientCapabilities.SharedArrayBuffer) {
		return Created{}, ErrBlocked
	}
	if service.dependencies.Versions[emulatorVersion] == nil {
		return Created{}, ErrBlocked
	}
	compatibility, err := service.loadArtifactCompatibility(ctx, artifactID)
	if err != nil {
		return Created{}, ErrBlocked
	}
	selectedDOSEntry := request.DOSEntry
	if request.SaveStateID != nil && savedDOSEntry.Valid {
		selectedDOSEntry = &savedDOSEntry.String
	}
	if selectedDOSEntry != nil {
		var directLaunchSafe int
		err := service.database.QueryRowContext(ctx, `
SELECT d.direct_launch_safe
FROM game_variant_revisions r
JOIN dos_entries d ON d.game_content_revision_id=r.game_content_revision_id
WHERE r.id=?
AND d.normalized_path=?
AND d.enabled=1
`, variantRevisionID, *selectedDOSEntry).
			Scan(&directLaunchSafe)
		if errors.Is(err, sql.ErrNoRows) {
			return Created{}, ErrDOSEntryMissing
		}
		if err != nil {
			return Created{}, fmt.Errorf("launch/service: %w", err)
		}
		if directLaunchSafe != 1 {
			return Created{}, ErrDOSEntryUnsafe
		}
	}
	contentPlan, err := service.buildLaunchContentPlan(ctx, variantRevisionID, selectedCore, compatibility)
	if err != nil {
		return Created{}, err
	}
	if contentPlan.ContentKind != contentKind {
		return Created{}, ErrBlocked
	}
	if err := service.validateLaunchLogicalNames(ctx, variantRevisionID, contentPlan.LogicalName); err != nil {
		return Created{}, err
	}
	initialDiscIndex := int64(0)
	if contentKind == corevalidation.MultiDiscContentKind {
		if request.SaveStateID != nil {
			if !savedDiscIndex.Valid || savedDiscIndex.Int64 < 0 || savedDiscIndex.Int64 >= int64(len(contentPlan.Discs)) {
				return Created{}, ErrBlocked
			}
			initialDiscIndex = savedDiscIndex.Int64
		} else if savedDiscIndex.Valid {
			return Created{}, ErrBlocked
		}
	} else if savedDiscIndex.Valid {
		return Created{}, ErrBlocked
	}
	var persistentBase sql.NullString
	if compatibility.PersistentSaveMode != "NONE" {
		baseErr := service.database.QueryRowContext(ctx, `
SELECT current_revision_id
FROM persistent_saves
WHERE profile_id=?
AND game_variant_revision_id=?
AND kind=?
	`, profileID, variantRevisionID, *compatibility.PersistentSaveKind).
			Scan(&persistentBase)
		if baseErr != nil && !errors.Is(baseErr, sql.ErrNoRows) {
			return Created{}, fmt.Errorf("launch/service: %w", baseErr)
		}
	}
	launchID, err := uuid.NewV7()
	if err != nil {
		return Created{}, fmt.Errorf("launch/service: %w", err)
	}
	capability := service.credentials.Capability(launchID)
	capabilityHash := retromruntime.HashCapability(capability)
	now := service.now().UnixMilli()
	bootstrapExpires := now + int64(5*time.Minute/time.Millisecond)
	hardExpires := now + int64(24*time.Hour/time.Millisecond)
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Created{}, fmt.Errorf("launch/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO launch_sessions(id,
profile_id,
game_id,
game_variant_revision_id,
core_artifact_id,
save_state_id,
dos_entry_path,
persistent_save_base_revision_id,
initial_disc_index,
return_to,
credential_sha256,
state,
bootstrap_expires_at_ms,
hard_expires_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
'CREATED',
?,
?,
?,
?)
`,
		launchID.String(),
		profileID,
		request.GameID,
		variantRevisionID,
		artifactID,
		request.SaveStateID,
		selectedDOSEntry,
		persistentBase,
		initialDiscIndex,
		request.ReturnTo,
		capabilityHash[:],
		bootstrapExpires,
		hardExpires,
		now,
		now,
	)
	if err != nil {
		return Created{}, fmt.Errorf("create launch session: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_content_files(launch_session_id,
logical_name,
blob_id,
format_version,
created_at_ms) VALUES(?,
?,
?,
?,
?)
`, launchID.String(), contentPlan.LogicalName, contentPlan.BlobID, contentPlan.Format, now); err != nil {
		return Created{}, fmt.Errorf("lock launch content: %w", err)
	}
	for _, disc := range contentPlan.Discs {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_external_files(launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind)
VALUES(?,?,?,?,?,'DISC')
`, launchID.String(), disc.VirtualPath, disc.LogicalName, disc.BlobID, now); err != nil {
			return Created{}, fmt.Errorf("lock launch disc: %w", err)
		}
	}
	if err := service.lockExternalBIOS(
		ctx, transaction, launchID.String(), variantRevisionID, now,
		revisionCompatibilityCode == reviewScreenshotOverrideCode,
	); err != nil {
		return Created{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Created{}, fmt.Errorf("commit launch session: %w", err)
	}
	return Created{
		LaunchID:             launchID.String(),
		PlayURL:              "/play/" + launchID.String(),
		Warnings:             []string{},
		BootstrapExpiresAtMS: bootstrapExpires,
		HardExpiresAtMS:      hardExpires,
		Capability:           retromruntime.EncodeCapability(capability),
	}, nil
}

func (service *Service) loadArtifactCompatibility(
	ctx context.Context,
	artifactID string,
) (artifactCompatibility, error) {
	var raw string
	if err := service.database.QueryRowContext(ctx, `
SELECT compatibility_config_json
FROM core_artifacts
WHERE id=?
`, artifactID).Scan(&raw); err != nil {
		return artifactCompatibility{}, fmt.Errorf("launch/artifact compatibility: %w", err)
	}
	var compatibility artifactCompatibility
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compatibility); err != nil || !validArtifactCompatibility(compatibility) {
		return artifactCompatibility{}, ErrBlocked
	}
	return compatibility, nil
}

func validArtifactCompatibility(compatibility artifactCompatibility) bool {
	if !validArtifactCompatibilitySchema(compatibility) || compatibility.DefaultOptions == nil ||
		compatibility.StartupActions == nil {
		return false
	}
	if len(compatibility.DefaultOptions) > 32 || len(compatibility.StartupActions) > 4 {
		return false
	}
	if !validRuntimeCoreID(compatibility.RuntimeCoreID) ||
		!validRequestedArtifactBasename(compatibility.RequestedArtifactBasename) {
		return false
	}
	if compatibility.CanvasResizePolicy != "NONE" &&
		compatibility.CanvasResizePolicy != "ON_GAME_START_TO_CSS_PIXELS" {
		return false
	}
	if compatibility.InputMode != "STANDARD" && compatibility.InputMode != "POINTER" {
		return false
	}
	return validPersistentCapability(compatibility) &&
		validDefaultOptions(compatibility.DefaultOptions) &&
		validStartupActions(compatibility.StartupActions)
}

func validArtifactCompatibilitySchema(compatibility artifactCompatibility) bool {
	switch compatibility.SchemaVersion {
	case 2:
		return len(compatibility.SupportedContentKinds) == 0 && compatibility.MultiDisc == nil
	case 3:
		return validContentCapabilities(compatibility)
	default:
		return false
	}
}

func validContentCapabilities(compatibility artifactCompatibility) bool {
	if len(compatibility.SupportedContentKinds) != 1 && len(compatibility.SupportedContentKinds) != 2 {
		return false
	}
	for index, kind := range compatibility.SupportedContentKinds {
		if kind != "SINGLE_FILE" && kind != "DOS_BUNDLE" && kind != "MULTI_DISC_M3U_V1" ||
			index > 0 && kind == compatibility.SupportedContentKinds[index-1] {
			return false
		}
	}
	if compatibility.MultiDisc == nil {
		return !slices.Contains(compatibility.SupportedContentKinds, "MULTI_DISC_M3U_V1")
	}
	return slices.Contains(compatibility.SupportedContentKinds, "MULTI_DISC_M3U_V1") &&
		compatibility.MultiDisc.MaxDiscs >= 2 && compatibility.MultiDisc.MaxDiscs <= 8 &&
		compatibility.MultiDisc.MaxTotalBytes >= 1 && compatibility.MultiDisc.MaxTotalBytes <= 1_073_741_824 &&
		compatibility.MultiDisc.Delivery == "EAGER_EXTERNAL_FILES"
}

func validRuntimeCoreID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character != '_' && (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validRequestedArtifactBasename(value string) bool {
	return path.Base(value) == value && !strings.Contains(value, `\`) &&
		!strings.Contains(value, "..") && strings.HasSuffix(value, "-wasm.data")
}

func validPersistentCapability(compatibility artifactCompatibility) bool {
	switch compatibility.PersistentSaveMode {
	case "SINGLE_FILE":
		return compatibility.PersistentSaveKind != nil && *compatibility.PersistentSaveKind == "CORE_SAVE"
	case "DOS_OVERLAY":
		return compatibility.PersistentSaveKind != nil && *compatibility.PersistentSaveKind == "DOS_OVERLAY"
	case "NONE":
		return compatibility.PersistentSaveKind == nil
	default:
		return false
	}
}

func validDefaultOptions(options map[string]string) bool {
	for name, value := range options {
		if name == "__proto__" || name == "prototype" || name == "constructor" ||
			!printableASCII(name, 1, 128) || !printableASCII(value, 0, 128) {
			return false
		}
	}
	return true
}

func validStartupActions(actions []dependencies.StartupAction) bool {
	for _, action := range actions {
		if action.Event != "GAME_START" || action.Kind != "PRESS_CONTROL" ||
			action.DelayMS < 0 || action.DelayMS > 30_000 || action.Player < 0 || action.Player > 3 ||
			action.Control < 0 || action.Control > 255 || action.DurationMS < 1 || action.DurationMS > 1_000 {
			return false
		}
	}
	return true
}

func printableASCII(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func (service *Service) lockExternalBIOS(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, variantRevisionID string,
	now int64,
	allowMissing bool,
) error {
	var snapshotJSON, contentLogicalName string
	if err := transaction.QueryRowContext(ctx, `
SELECT revision.dependency_snapshot_json,content.logical_name
FROM game_variant_revisions revision
JOIN launch_content_files content ON content.launch_session_id=?
WHERE revision.id=?
`, launchID, variantRevisionID).Scan(&snapshotJSON, &contentLogicalName); err != nil {
		return ErrBlocked
	}
	dependencies, err := corevalidation.ParseRuntimeBIOSDependencies(snapshotJSON)
	if err != nil {
		return ErrBlocked
	}
	seenLogicalNames, seenVirtualPaths, count, err := lockedExternalNames(
		ctx, transaction, launchID, contentLogicalName,
	)
	if err != nil {
		return err
	}
	for _, dependency := range dependencies {
		if dependency.DeliveryKind != "EXTERNAL_FILE" {
			continue
		}
		if !availableExternalBIOS(dependency) {
			if allowMissing {
				continue
			}
			return ErrBlocked
		}
		count++
		if count > 16 {
			return ErrBlocked
		}
		logicalKey := strings.ToLower(dependency.LogicalName)
		if _, duplicate := seenLogicalNames[logicalKey]; duplicate {
			return ErrBlocked
		}
		if _, duplicate := seenVirtualPaths[*dependency.EmulatorPath]; duplicate {
			return ErrBlocked
		}
		seenLogicalNames[logicalKey] = struct{}{}
		seenVirtualPaths[*dependency.EmulatorPath] = struct{}{}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO launch_external_files(launch_session_id,
virtual_path,
logical_name,
blob_id,
created_at_ms,
kind) VALUES(?,?,?,?,?,'BIOS')
`, launchID, *dependency.EmulatorPath, dependency.LogicalName, *dependency.BlobID, now); err != nil {
			return fmt.Errorf("lock launch external BIOS: %w", err)
		}
	}
	return nil
}

func availableExternalBIOS(dependency corevalidation.BIOSDependency) bool {
	return dependency.EmulatorPath != nil && dependency.BlobID != nil && dependency.InstallationStatus != nil &&
		(*dependency.InstallationStatus == "MATCHED" || *dependency.InstallationStatus == "HASH_WARNING")
}

func lockedExternalNames(
	ctx context.Context,
	transaction *sql.Tx,
	launchID, contentLogicalName string,
) (map[string]struct{}, map[string]struct{}, int, error) {
	seenLogicalNames := map[string]struct{}{strings.ToLower(contentLogicalName): {}}
	seenVirtualPaths := make(map[string]struct{})
	existingRows, err := transaction.QueryContext(ctx, `
SELECT virtual_path,logical_name
FROM launch_external_files
WHERE launch_session_id=?
ORDER BY virtual_path
`, launchID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("lock launch external files: %w", err)
	}
	defer func() { cleanup.Error("close", existingRows.Close()) }()
	count := 0
	for existingRows.Next() {
		var virtualPath, logicalName string
		if err := existingRows.Scan(&virtualPath, &logicalName); err != nil {
			return nil, nil, 0, fmt.Errorf("lock launch external files: %w", err)
		}
		logicalKey := strings.ToLower(logicalName)
		if _, duplicate := seenLogicalNames[logicalKey]; duplicate {
			return nil, nil, 0, ErrBlocked
		}
		if _, duplicate := seenVirtualPaths[virtualPath]; duplicate {
			return nil, nil, 0, ErrBlocked
		}
		seenLogicalNames[logicalKey] = struct{}{}
		seenVirtualPaths[virtualPath] = struct{}{}
		count++
	}
	if err := existingRows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("lock launch external files: %w", err)
	}
	return seenLogicalNames, seenVirtualPaths, count, nil
}

func (service *Service) validateLaunchLogicalNames(
	ctx context.Context,
	variantRevisionID, contentLogicalName string,
) error {
	seen := map[string]struct{}{strings.ToLower(contentLogicalName): {}}
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT logical_name
FROM variant_files
WHERE game_variant_revision_id=?
AND role IN ('PARENT',
'BIOS_BUNDLE')
ORDER BY role,
sort_order,
logical_name
`,
		variantRevisionID,
	)
	if err != nil {
		return fmt.Errorf("launch/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var logicalName string
		if err := rows.Scan(&logicalName); err != nil {
			return fmt.Errorf("launch/service: %w", err)
		}
		key := strings.ToLower(logicalName)
		if _, exists := seen[key]; exists || logicalName == "" || path.Base(logicalName) != logicalName ||
			strings.Contains(logicalName, `\`) {
			return ErrBlocked
		}
		seen[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan launch content files: %w", err)
	}
	return nil
}

func (service *Service) lockLaunchContent(
	ctx context.Context,
	variantRevisionID, coreID string,
) (string, string, string, error) {
	if coreID != "dosbox_pure" {
		var blobID, logicalName string
		err := service.database.QueryRowContext(ctx, `
SELECT f.blob_id,
f.logical_name
FROM game_variant_revisions r
JOIN game_content_files f ON f.game_content_revision_id=r.game_content_revision_id
AND f.role='CONTENT'
WHERE r.id=?
ORDER BY f.sort_order,
f.logical_name LIMIT 1
`, variantRevisionID).
			Scan(&blobID, &logicalName)
		if err != nil {
			return "", "", "", ErrBlocked
		}
		return blobID, logicalName, "SOURCE_V1", nil
	}
	var baseBlobID string
	err := service.database.QueryRowContext(ctx, `
SELECT vf.blob_id
FROM variant_files vf
WHERE vf.game_variant_revision_id=?
AND vf.role='DOS_LAUNCH_BUNDLE'
AND vf.logical_name='game.zip'
`, variantRevisionID).
		Scan(&baseBlobID)
	if err != nil {
		return "", "", "", ErrBlocked
	}
	return baseBlobID, "game.zip", "RETROM_DOS_DIRECT_ZIP_V1", nil
}

func validReturnTo(value, gameID string) bool {
	if strings.ContainsAny(value, "?#%\\") {
		return false
	}
	return value == "/" || value == "/library" || value == "/saves" || value == "/games/"+gameID
}

type Config struct {
	Mode                 string                       `json:"mode"`
	LaunchID             string                       `json:"launchId"`
	EmulatorJSVersion    string                       `json:"emulatorjsVersion"`
	PlayerAdapterID      string                       `json:"playerAdapterId"`
	Core                 string                       `json:"core"`
	RuntimeCore          string                       `json:"runtimeCore"`
	CoreName             string                       `json:"coreName"`
	CoreArtifactID       string                       `json:"coreArtifactId"`
	EmulatorGameID       int64                        `json:"emulatorGameId"`
	GameName             string                       `json:"gameName"`
	GameTitle            string                       `json:"gameTitle"`
	PlatformName         string                       `json:"platformName"`
	RuntimeBaseURL       string                       `json:"runtimeBaseUrl"`
	LoaderURL            string                       `json:"loaderUrl"`
	GameURL              string                       `json:"gameUrl"`
	BIOSURL              any                          `json:"biosUrl"`
	ParentURL            any                          `json:"parentUrl"`
	StateURL             any                          `json:"stateUrl"`
	PersistentSaveMode   string                       `json:"persistentSaveMode"`
	PersistentSaveURL    *string                      `json:"persistentSaveUrl"`
	InputMode            string                       `json:"inputMode"`
	StartupActions       []dependencies.StartupAction `json:"startupActions"`
	RequiresThreads      bool                         `json:"requiresThreads"`
	RuntimePathOverrides map[string]string            `json:"runtimePathOverrides"`
	DefaultCoreOptions   map[string]string            `json:"defaultCoreOptions"`
	ExternalFiles        map[string]string            `json:"externalFiles"`
	DiscSet              *DiscSet                     `json:"discSet"`
	DOSEntry             any                          `json:"dosEntry"`
	Warnings             []string                     `json:"warnings"`
	ReturnTo             string                       `json:"returnTo"`
	ReviewPreview        *ReviewPreviewConfig         `json:"reviewPreview,omitempty"`
	Netplay              *NetplayConfig               `json:"netplay"`
}

type NetplayConfig struct {
	RoomID           string          `json:"roomId"`
	SessionID        string          `json:"sessionId"`
	PlayerNo         int             `json:"playerNo"`
	NetplayProfile   json.RawMessage `json:"netplayProfile"`
	RuntimeSocketURL string          `json:"runtimeSocketUrl"`
}

type DiscSet struct {
	ContentKind      string      `json:"contentKind"`
	Count            int         `json:"count"`
	InitialDiscIndex int         `json:"initialDiscIndex"`
	Entries          []DiscEntry `json:"entries"`
}

type DiscEntry struct {
	Index       int    `json:"index"`
	Label       string `json:"label"`
	VirtualPath string `json:"virtualPath"`
}

type MultiDiscTelemetryDimensions struct {
	PlatformKey     string
	CoreKey         string
	ArtifactVersion int64
	DiscCount       int
}

func (service *Service) MultiDiscTelemetryDimensions(
	ctx context.Context,
	launchID, capability string,
) (MultiDiscTelemetryDimensions, error) {
	var credentialHash []byte
	var state, platformKey, coreKey, contentFormat string
	var hardExpires, artifactVersion int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,
launch.state,
launch.hard_expires_at_ms,
platform.id,
artifact.core_id,
artifact.version,
content.format_version,
(SELECT count(*) FROM launch_external_files file
 WHERE file.launch_session_id=launch.id AND file.kind='DISC')
FROM launch_sessions launch
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN launch_content_files content ON content.launch_session_id=launch.id
WHERE launch.id=?
`, launchID).Scan(
		&credentialHash, &state, &hardExpires, &platformKey, &coreKey,
		&artifactVersion, &contentFormat, &discCount,
	)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() || state != "ACTIVE" ||
		contentFormat != "RETROM_MULTIDISC_M3U_V1" || discCount < 2 || discCount > 8 {
		return MultiDiscTelemetryDimensions{}, ErrCredential
	}
	return MultiDiscTelemetryDimensions{
		PlatformKey: platformKey, CoreKey: coreKey,
		ArtifactVersion: artifactVersion, DiscCount: discCount,
	}, nil
}

type BundleFile struct {
	LogicalName string
	SHA256      string
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Config(ctx context.Context, launchID, capability string) (Config, error) {
	var credentialHash []byte
	var state, coreID, coreName, artifactID, emulatorVersion, relativePath, compatibilityJSON string
	var dependencySnapshotJSON, variantRevisionID, revisionCompatibilityCode string
	var gameTitle, platformName string
	var logicalName, contentFormat, returnTo string
	var bootstrapExpires, hardExpires, emulatorGameID, initialDiscIndex int64
	var requiresThreads int
	var saveStateID, dosEntry sql.NullString
	var idleExpires sql.NullInt64
	var netplaySessionID, netplayRoomID, netplayProfileJSON sql.NullString
	var netplayPlayerNo sql.NullInt64
	var saveAccess string
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.bootstrap_expires_at_ms,
l.hard_expires_at_ms,
l.idle_expires_at_ms,
a.core_id,
a.id,
a.emulatorjs_version,
a.relative_path,
a.compatibility_config_json,
c.requires_threads,
c.name,
r.emulator_game_id,
r.dependency_snapshot_json,
r.compatibility_code,
l.game_variant_revision_id,
metadata.title,
platform.name,
lc.logical_name,
lc.format_version,
l.return_to,
l.save_state_id,
l.dos_entry_path,
l.initial_disc_index,
l.netplay_session_id,
l.netplay_player_no,
l.save_access,
session.room_id,
session.profile_json
FROM launch_sessions l
JOIN core_artifacts a ON a.id=l.core_artifact_id
JOIN cores c ON c.id=a.core_id
JOIN game_variant_revisions r ON r.id=l.game_variant_revision_id
JOIN games g ON g.id=l.game_id
JOIN game_metadata_revisions metadata ON metadata.id=g.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=g.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN launch_content_files lc ON lc.launch_session_id=l.id
LEFT JOIN netplay_sessions session ON session.id=l.netplay_session_id
WHERE l.id=?
`, launchID).
		Scan(
			&credentialHash,
			&state,
			&bootstrapExpires,
			&hardExpires,
			&idleExpires,
			&coreID,
			&artifactID,
			&emulatorVersion,
			&relativePath,
			&compatibilityJSON,
			&requiresThreads,
			&coreName,
			&emulatorGameID,
			&dependencySnapshotJSON,
			&revisionCompatibilityCode,
			&variantRevisionID,
			&gameTitle,
			&platformName,
			&logicalName,
			&contentFormat,
			&returnTo,
			&saveStateID,
			&dosEntry,
			&initialDiscIndex,
			&netplaySessionID,
			&netplayPlayerNo,
			&saveAccess,
			&netplayRoomID,
			&netplayProfileJSON,
		)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) {
		return Config{}, ErrCredential
	}
	isNetplay := netplaySessionID.Valid
	validNetplayFields := netplayPlayerNo.Valid && netplayRoomID.Valid &&
		netplayProfileJSON.Valid && saveAccess == "NETPLAY_DISABLED"
	if isNetplay != validNetplayFields ||
		!isNetplay && saveAccess != "NORMAL" ||
		isNetplay && (saveStateID.Valid || dosEntry.Valid || netplayPlayerNo.Int64 < 1 || netplayPlayerNo.Int64 > 4) {
		return Config{}, ErrCredential
	}
	now := service.now().UnixMilli()
	if hardExpires <= now || state == "CREATED" && bootstrapExpires <= now ||
		idleExpires.Valid && idleExpires.Int64 <= now ||
		state == "FINISHED" ||
		state == "EXPIRED" ||
		state == "REVOKED" {
		return Config{}, ErrCredential
	}
	if state == "CREATED" {
		if _, err := service.database.ExecContext(ctx, `
UPDATE launch_sessions
SET state='ACTIVE',
activated_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
AND state='CREATED'
`, now, now, launchID); err != nil {
			return Config{}, fmt.Errorf("launch/service: %w", err)
		}
	}
	version := service.dependencies.Versions[emulatorVersion]
	if version == nil {
		return Config{}, ErrCredential
	}
	base := "/runtime/emulatorjs/" + emulatorVersion + "/"
	var compatibility artifactCompatibility
	decoder := json.NewDecoder(strings.NewReader(compatibilityJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compatibility); err != nil || !validArtifactCompatibility(compatibility) {
		return Config{}, ErrCredential
	}
	overrides := map[string]string{compatibility.RequestedArtifactBasename: base + relativePath}
	stateURL := any(nil)
	if saveStateID.Valid && !isNetplay {
		stateURL = "/runtime/launches/" + launchID + "/state"
	}
	biosURL, parentURL := any(nil), any(nil)
	biosFiles, _ := service.BundleFiles(ctx, launchID, capability, "BIOS_BUNDLE")
	parentFiles, _ := service.BundleFiles(ctx, launchID, capability, "PARENT")
	if len(biosFiles) > 0 {
		biosURL = "/runtime/launches/" + launchID + "/bios/bundle.zip"
	}
	if len(parentFiles) > 0 {
		parentURL = "/runtime/launches/" + launchID + "/parent/bundle.zip"
	}
	coreOptions := make(map[string]string, len(compatibility.DefaultOptions)+4)
	for name, value := range compatibility.DefaultOptions {
		coreOptions[name] = value
	}
	if existing, ok := coreOptions["webgl2Enabled"]; ok && existing != "enabled" {
		return Config{}, ErrBlocked
	}
	coreOptions["webgl2Enabled"] = "enabled"
	warnings := make([]string, 0)
	if revisionCompatibilityCode == reviewScreenshotOverrideCode {
		warnings = append(warnings, reviewScreenshotOverrideCode)
	}
	biosDependencies, snapshotErr := corevalidation.ParseRuntimeBIOSDependencies(dependencySnapshotJSON)
	if snapshotErr != nil {
		return Config{}, ErrBlocked
	}
	for _, dependency := range biosDependencies {
		if dependency.BlobID == nil || dependency.InstallationStatus == nil {
			continue
		}
		for name, value := range dependency.ActivationOptions {
			if existing, ok := coreOptions[name]; ok && existing != value {
				return Config{}, ErrBlocked
			}
			coreOptions[name] = value
		}
		if *dependency.InstallationStatus == "HASH_WARNING" {
			warnings = append(warnings, "BIOS_HASH_WARNING")
		}
	}
	externalFiles := make(map[string]string)
	externalRows, externalErr := service.database.QueryContext(ctx, `
SELECT virtual_path,
logical_name,
kind
FROM launch_external_files
WHERE launch_session_id=?
ORDER BY CASE kind WHEN 'DISC' THEN 0 ELSE 1 END,virtual_path
`, launchID)
	if externalErr != nil {
		return Config{}, fmt.Errorf("launch/service: %w", externalErr)
	}
	defer func() { cleanup.Error("close", externalRows.Close()) }()
	discEntries := make([]DiscEntry, 0, 8)
	for externalRows.Next() {
		var virtualPath, externalName, kind string
		if err := externalRows.Scan(&virtualPath, &externalName, &kind); err != nil || len(externalFiles) >= 16 {
			return Config{}, ErrBlocked
		}
		if _, duplicate := externalFiles[virtualPath]; duplicate {
			return Config{}, ErrBlocked
		}
		if kind == "DISC" {
			index := len(discEntries)
			expectedName := fmt.Sprintf("disc-%03d.chd", index+1)
			if externalName != expectedName || virtualPath != "/"+expectedName {
				return Config{}, ErrBlocked
			}
			discEntries = append(discEntries, DiscEntry{
				Index: index, Label: fmt.Sprintf("光盘 %d", index+1), VirtualPath: virtualPath,
			})
		} else if kind != "BIOS" {
			return Config{}, ErrBlocked
		}
		externalFiles[virtualPath] = "/runtime/launches/" + launchID + "/external-files/" + url.PathEscape(externalName)
	}
	if err := externalRows.Err(); err != nil {
		return Config{}, fmt.Errorf("launch/service: %w", err)
	}
	if !configureDOSLaunch(coreID, contentFormat, dosEntry, externalFiles, coreOptions) {
		return Config{}, ErrBlocked
	}
	var discSet *DiscSet
	if contentFormat == "RETROM_MULTIDISC_M3U_V1" {
		if len(discEntries) < 2 || initialDiscIndex < 0 || initialDiscIndex >= int64(len(discEntries)) ||
			logicalName != "playlist.m3u" {
			return Config{}, ErrBlocked
		}
		discSet = &DiscSet{
			ContentKind: corevalidation.MultiDiscContentKind, Count: len(discEntries),
			InitialDiscIndex: int(initialDiscIndex), Entries: discEntries,
		}
	} else if len(discEntries) != 0 || initialDiscIndex != 0 {
		return Config{}, ErrBlocked
	}
	var persistentSaveURL *string
	persistentSaveMode := compatibility.PersistentSaveMode
	if isNetplay {
		persistentSaveMode = "NONE"
	}
	if persistentSaveMode != "NONE" {
		value := "/runtime/launches/" + launchID + "/persistent-save"
		persistentSaveURL = &value
	}
	startupActions := make([]dependencies.StartupAction, len(compatibility.StartupActions))
	copy(startupActions, compatibility.StartupActions)
	mode := "single"
	var netplayConfig *NetplayConfig
	if isNetplay {
		mode = "netplay"
		var profileObject map[string]any
		if json.Unmarshal([]byte(netplayProfileJSON.String), &profileObject) != nil ||
			profileObject["coreArtifactId"] != artifactID ||
			profileObject["gameVariantRevisionId"] != variantRevisionID {
			return Config{}, ErrCredential
		}
		netplayConfig = &NetplayConfig{
			RoomID: netplayRoomID.String, SessionID: netplaySessionID.String, PlayerNo: int(netplayPlayerNo.Int64),
			NetplayProfile:   json.RawMessage(netplayProfileJSON.String),
			RuntimeSocketURL: "/runtime/netplay/rooms/" + netplayRoomID.String + "/socket",
		}
	}
	return Config{
		Mode:              mode,
		LaunchID:          launchID,
		EmulatorJSVersion: emulatorVersion,
		PlayerAdapterID:   version.Manifest.EmulatorJS.PlayerAdapter.ID,
		Core:              coreID,
		RuntimeCore:       compatibility.RuntimeCoreID,
		CoreName:          coreName,
		CoreArtifactID:    artifactID,
		EmulatorGameID:    emulatorGameID,
		GameName:          fmt.Sprintf("retrom-%d", emulatorGameID),
		GameTitle:         gameTitle,
		PlatformName:      platformName,
		RuntimeBaseURL: base + strings.TrimSuffix(
			version.Manifest.EmulatorJS.PlayerAdapter.RuntimeBasePath,
			"/",
		) + "/",
		LoaderURL:            base + version.Manifest.EmulatorJS.PlayerAdapter.LoaderPath,
		GameURL:              "/runtime/launches/" + launchID + "/game/" + url.PathEscape(logicalName),
		BIOSURL:              biosURL,
		ParentURL:            parentURL,
		StateURL:             stateURL,
		PersistentSaveMode:   persistentSaveMode,
		PersistentSaveURL:    persistentSaveURL,
		InputMode:            compatibility.InputMode,
		StartupActions:       startupActions,
		RequiresThreads:      requiresThreads == 1,
		RuntimePathOverrides: overrides,
		DefaultCoreOptions:   coreOptions,
		ExternalFiles:        externalFiles,
		DiscSet:              discSet,
		DOSEntry:             nullableString(dosEntry),
		Warnings:             warnings,
		ReturnTo:             returnTo,
		Netplay:              netplayConfig,
	}, nil
}

func configureDOSLaunch(
	coreID, contentFormat string,
	dosEntry sql.NullString,
	externalFiles, coreOptions map[string]string,
) bool {
	if coreID != "dosbox_pure" || !dosEntry.Valid {
		return true
	}
	return contentFormat == "RETROM_DOS_DIRECT_ZIP_V1" &&
		externalFiles["/game.conf"] == "" && coreOptions["dosbox_pure_conf"] == ""
}

func (service *Service) BundleFiles(ctx context.Context, launchID, capability, kind string) ([]BundleFile, error) {
	if kind != "BIOS_BUNDLE" && kind != "PARENT" {
		return nil, ErrCredential
	}
	var credentialHash []byte
	var state string
	var hardExpires int64
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.hard_expires_at_ms
FROM launch_sessions l
WHERE l.id=?
`, launchID).
		Scan(&credentialHash, &state, &hardExpires)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() ||
		state != "ACTIVE" {
		return nil, ErrCredential
	}
	rows, err := service.database.QueryContext(
		ctx,
		`
SELECT vf.logical_name,
b.sha256
FROM launch_sessions l
JOIN variant_files vf ON vf.game_variant_revision_id=l.game_variant_revision_id
JOIN blobs b ON b.id=vf.blob_id
WHERE l.id=?
AND vf.role=?
ORDER BY vf.sort_order,
vf.logical_name
`,
		launchID,
		kind,
	)
	if err != nil {
		return nil, fmt.Errorf("launch/service: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	files := make([]BundleFile, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var file BundleFile
		if err := rows.Scan(&file.LogicalName, &file.SHA256); err != nil {
			return nil, fmt.Errorf("launch/service: %w", err)
		}
		if _, duplicate := seen[file.LogicalName]; !duplicate {
			files = append(files, file)
			seen[file.LogicalName] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("launch/service: %w", err)
	}
	slices.SortFunc(
		files,
		func(left, right BundleFile) int { return strings.Compare(left.LogicalName, right.LogicalName) },
	)
	return files, nil
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func (service *Service) ContentBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	content, err := service.Content(ctx, launchID, capability, logicalName)
	return content.Digest, err
}

type ContentView struct {
	Digest          string
	Format          string
	CoreID          string
	DOSEntry        *string
	PlatformKey     string
	ArtifactVersion int64
	DiscCount       int
}

func (service *Service) Content(ctx context.Context, launchID, capability, logicalName string) (ContentView, error) {
	var credentialHash []byte
	var digest, state, format, coreID, platformKey string
	var dosEntry sql.NullString
	var hardExpires, artifactVersion int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT l.credential_sha256,
l.state,
l.hard_expires_at_ms,
b.sha256,
lc.format_version,
a.core_id,
a.version,
platform.id,
l.dos_entry_path,
(SELECT count(*) FROM launch_external_files file
 WHERE file.launch_session_id=l.id AND file.kind='DISC')
FROM launch_sessions l
JOIN launch_content_files lc ON lc.launch_session_id=l.id
JOIN blobs b ON b.id=lc.blob_id
JOIN core_artifacts a ON a.id=l.core_artifact_id
JOIN games game ON game.id=l.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE l.id=?
AND lc.logical_name=?
`, launchID, logicalName).Scan(
		&credentialHash, &state, &hardExpires, &digest, &format, &coreID,
		&artifactVersion, &platformKey, &dosEntry, &discCount,
	)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() ||
		state != "ACTIVE" {
		return ContentView{}, ErrCredential
	}
	var selected *string
	if dosEntry.Valid {
		selected = &dosEntry.String
	}
	return ContentView{
		Digest: digest, Format: format, CoreID: coreID, DOSEntry: selected,
		PlatformKey: platformKey, ArtifactVersion: artifactVersion, DiscCount: discCount,
	}, nil
}

type ExternalView struct {
	Digest          string
	Kind            string
	PlatformKey     string
	CoreKey         string
	ArtifactVersion int64
	DiscCount       int
}

func (service *Service) External(
	ctx context.Context,
	launchID, capability, logicalName string,
) (ExternalView, error) {
	var credentialHash []byte
	var digest, state, kind, platformKey, coreKey string
	var hardExpires, artifactVersion int64
	var discCount int
	err := service.database.QueryRowContext(ctx, `
SELECT launch.credential_sha256,
launch.state,
launch.hard_expires_at_ms,
blob.sha256,
file.kind,
platform.id,
artifact.core_id,
artifact.version,
(SELECT count(*) FROM launch_external_files disc
 WHERE disc.launch_session_id=launch.id AND disc.kind='DISC')
FROM launch_sessions launch
JOIN launch_external_files file ON file.launch_session_id=launch.id
JOIN blobs blob ON blob.id=file.blob_id
JOIN core_artifacts artifact ON artifact.id=launch.core_artifact_id
JOIN games game ON game.id=launch.game_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE launch.id=?
AND file.logical_name=?
`, launchID, logicalName).Scan(
		&credentialHash, &state, &hardExpires, &digest, &kind, &platformKey,
		&coreKey, &artifactVersion, &discCount,
	)
	if err != nil || !retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= service.now().UnixMilli() || state != "ACTIVE" {
		return ExternalView{}, ErrCredential
	}
	return ExternalView{
		Digest: digest, Kind: kind, PlatformKey: platformKey, CoreKey: coreKey,
		ArtifactVersion: artifactVersion, DiscCount: discCount,
	}, nil
}

func (service *Service) ExternalBlob(ctx context.Context, launchID, capability, logicalName string) (string, error) {
	view, err := service.External(ctx, launchID, capability, logicalName)
	if err != nil {
		return "", ErrCredential
	}
	return view.Digest, nil
}

type Interval struct {
	Running bool `json:"running"`
	Visible bool `json:"visible"`
	Paused  bool `json:"paused"`
}

type PlayEvent struct {
	ClientSequence     int64     `json:"clientSequence"`
	ClientObservedAtMS int64     `json:"clientObservedAtMs"`
	PreviousInterval   *Interval `json:"previousInterval"`
}

type PlayResult struct {
	PlaySessionID    any    `json:"playSessionId"`
	ClientSequence   int64  `json:"clientSequence"`
	AcceptedDuration int64  `json:"acceptedDurationMs"`
	State            string `json:"state"`
}

//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (service *Service) RecordPlay(
	ctx context.Context,
	launchID, capability, kind string,
	event PlayEvent,
) (PlayResult, error) {
	if event.ClientObservedAtMS < 0 || event.ClientObservedAtMS > 253402300799999 {
		return PlayResult{}, ErrBlocked
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var credentialHash []byte
	var launchState, profileID, gameID, variantRevisionID string
	var hardExpires int64
	var idleExpires sql.NullInt64
	now := service.now().UnixMilli()
	if err := transaction.QueryRowContext(ctx, `
SELECT credential_sha256,
state,
profile_id,
game_id,
game_variant_revision_id,
hard_expires_at_ms,
idle_expires_at_ms
FROM launch_sessions
WHERE id=?
`, launchID).Scan(
		&credentialHash,
		&launchState,
		&profileID,
		&gameID,
		&variantRevisionID,
		&hardExpires,
		&idleExpires,
	); err != nil ||
		!retromruntime.MatchesCapability(capability, credentialHash) ||
		hardExpires <= now {
		return PlayResult{}, ErrCredential
	}
	var playID, playState string
	var lastSequence, lastHeartbeat int64
	err = transaction.QueryRowContext(ctx, `
SELECT id,
state,
last_client_sequence,
last_heartbeat_at_ms
FROM play_sessions
WHERE launch_session_id=?
`, launchID).
		Scan(&playID, &playState, &lastSequence, &lastHeartbeat)
	if err == nil && event.ClientSequence <= lastSequence {
		var storedKind string
		var storedObserved, accepted int64
		var running, visible, paused bool
		if replayErr := transaction.QueryRowContext(ctx, `
SELECT event_kind,
client_observed_at_ms,
running,
visible,
paused,
accepted_duration_ms
FROM play_session_events
WHERE play_session_id=?
AND client_sequence=?
`, playID, event.ClientSequence).Scan(
			&storedKind,
			&storedObserved,
			&running,
			&visible,
			&paused,
			&accepted,
		); replayErr != nil {
			return PlayResult{}, ErrBlocked
		}
		expectedKind := "HEARTBEAT"
		switch kind {
		case "start":
			expectedKind = "START"
		case "finish":
			expectedKind = "FINISH"
		}
		intervalMatches := event.PreviousInterval == nil && storedKind == "START" ||
			event.PreviousInterval != nil && running == event.PreviousInterval.Running &&
				visible == event.PreviousInterval.Visible &&
				paused == event.PreviousInterval.Paused
		if storedKind != expectedKind || storedObserved != event.ClientObservedAtMS || !intervalMatches {
			return PlayResult{}, ErrBlocked
		}
		state := "ACTIVE"
		if storedKind == "FINISH" {
			state = "FINISHED"
		}
		return PlayResult{
			PlaySessionID:    playID,
			ClientSequence:   event.ClientSequence,
			AcceptedDuration: accepted,
			State:            state,
		}, nil
	}
	if kind == "start" {
		if event.ClientSequence != 0 || event.PreviousInterval != nil || launchState != "ACTIVE" {
			return PlayResult{}, ErrBlocked
		}
		if err == nil {
			return PlayResult{PlaySessionID: playID, ClientSequence: 0, AcceptedDuration: 0, State: playState}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		generated, _ := uuid.NewV7()
		playID = generated.String()
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_sessions(id,
launch_session_id,
profile_id,
game_id,
game_variant_revision_id,
started_at_ms,
last_heartbeat_at_ms,
active_duration_ms,
last_client_sequence,
state,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
0,
0,
'ACTIVE',
1,
?,
?)
`, playID, launchID, profileID, gameID, variantRevisionID, now, now, now, now); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_session_events(play_session_id,
client_sequence,
event_kind,
client_observed_at_ms,
server_received_at_ms,
running,
visible,
paused,
accepted_duration_ms,
created_at_ms) VALUES(?,
0,
'START',
?,
?,
0,
0,
0,
0,
?)
`, playID, event.ClientObservedAtMS, now, now); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET idle_expires_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now+int64(2*time.Minute/time.Millisecond), now, launchID); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		return PlayResult{PlaySessionID: playID, ClientSequence: 0, AcceptedDuration: 0, State: "ACTIVE"}, nil
	}
	if errors.Is(err, sql.ErrNoRows) && kind == "finish" && event.ClientSequence == 0 && event.PreviousInterval == nil {
		if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET state='FINISHED',
finished_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
AND state='ACTIVE'
`, now, now, launchID); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
		return PlayResult{PlaySessionID: nil, ClientSequence: 0, AcceptedDuration: 0, State: "FINISHED"}, nil
	}
	if err != nil || playState != "ACTIVE" || launchState != "ACTIVE" ||
		idleExpires.Valid && idleExpires.Int64 <= now ||
		event.PreviousInterval == nil ||
		event.ClientSequence != lastSequence+1 ||
		(kind != "heartbeat" && kind != "finish") {
		return PlayResult{}, ErrBlocked
	}
	accepted := int64(0)
	if event.PreviousInterval.Running && event.PreviousInterval.Visible && !event.PreviousInterval.Paused {
		accepted = min(now-lastHeartbeat, int64(45*time.Second/time.Millisecond))
		if accepted < 0 {
			accepted = 0
		}
	}
	eventKind := "HEARTBEAT"
	newState := "ACTIVE"
	var endedAt any
	if kind == "finish" {
		eventKind, newState, endedAt = "FINISH", "FINISHED", now
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO play_session_events(play_session_id,
client_sequence,
event_kind,
client_observed_at_ms,
server_received_at_ms,
running,
visible,
paused,
accepted_duration_ms,
created_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?)
`,
		playID,
		event.ClientSequence,
		eventKind,
		event.ClientObservedAtMS,
		now,
		event.PreviousInterval.Running,
		event.PreviousInterval.Visible,
		event.PreviousInterval.Paused,
		accepted,
		now,
	); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE play_sessions
SET last_heartbeat_at_ms=?,
ended_at_ms=?,
active_duration_ms=active_duration_ms+?,
last_client_sequence=?,
state=?,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now, endedAt, accepted, event.ClientSequence, newState, now, playID); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if kind == "finish" {
		if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET state='FINISHED',
finished_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now, now, launchID); err != nil {
			return PlayResult{}, fmt.Errorf("launch/service: %w", err)
		}
	} else if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions
SET idle_expires_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`, now+int64(2*time.Minute/time.Millisecond), now, launchID); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return PlayResult{}, fmt.Errorf("launch/service: %w", err)
	}
	return PlayResult{
		PlaySessionID:    playID,
		ClientSequence:   event.ClientSequence,
		AcceptedDuration: accepted,
		State:            newState,
	}, nil
}

func MarshalConfig(config Config) ([]byte, error) {
	contents, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal launch config: %w", err)
	}
	return contents, nil
}
