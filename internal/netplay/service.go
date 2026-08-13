package netplay

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/launch"
)

const (
	RoomStateDraft    = "DRAFT"
	RoomStateWaiting  = "WAITING"
	RoomStateStarting = "STARTING"
	RoomStateRunning  = "RUNNING"
)

var (
	ErrRoomNotFound    = errors.New("NETPLAY_ROOM_NOT_FOUND")
	ErrSessionNotFound = errors.New("NETPLAY_SESSION_NOT_FOUND")
	ErrForbidden       = errors.New("NETPLAY_FORBIDDEN")
	ErrInvalidSeat     = errors.New("NETPLAY_INVALID_SEAT")
	ErrInvalidProfile  = errors.New("NETPLAY_INVALID_PROFILE")
	ErrSeatTaken       = errors.New("NETPLAY_SEAT_TAKEN")
	ErrRoomNotReady    = errors.New("NETPLAY_ROOM_NOT_READY")
	ErrRoomConflict    = errors.New("NETPLAY_ROOM_STATE_CONFLICT")
	ErrProfileStale    = errors.New("NETPLAY_PROFILE_STALE")
	ErrCapacity        = errors.New("NETPLAY_CAPACITY_REACHED")
	ErrPrecondition    = errors.New("PRECONDITION_FAILED")
	errUUIDUnavailable = errors.New("netplay: UUID unavailable")
	errEventData       = errors.New("netplay: event data invalid")
)

func serviceError(operation string, err error) error {
	return fmt.Errorf("netplay/%s: %w", operation, err)
}

type Options struct {
	MaxActiveRooms int
	DraftIdle      time.Duration
	WaitingIdle    time.Duration
	ReconnectLease time.Duration
}

type Service struct {
	database    *sql.DB
	registry    *Registry
	credentials *Credentials
	clock       Clock
	options     Options
	stop        chan struct{}
	done        chan struct{}
	launchMu    sync.Mutex
}

const (
	RoomDispositionWaiting = "WAITING"
	RoomDispositionEnded   = "ENDED"
)

func endDisposition(reason string, actorIsHost bool) string {
	switch reason {
	case "HOST_CLOSED", "HOST_LOST", "PROFILE_REVOKED", "SERVER_RESTARTED", "RESTORE", "HARD_EXPIRED":
		return RoomDispositionEnded
	case "AUTH_REVOKED", "PEER_TIMEOUT", "PROTOCOL_VIOLATION":
		if actorIsHost {
			return RoomDispositionEnded
		}
	}
	return RoomDispositionWaiting
}

func NewService(
	database *sql.DB,
	registry *Registry,
	credentials *Credentials,
	options Options,
	now func() time.Time,
) *Service {
	clock := Clock(realClock{})
	if now != nil {
		clock = clockFunc(now)
	}
	return &Service{
		database: database, registry: registry, credentials: credentials, clock: clock, options: options,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

func (service *Service) StartMaintenance() {
	go func() {
		defer close(service.done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-service.stop:
				return
			case <-ticker.C:
				_ = service.ExpireRooms(context.Background())
			}
		}
	}()
}

func (service *Service) Close() {
	select {
	case <-service.stop:
		return
	default:
		close(service.stop)
		<-service.done
	}
}

type ProfileSummary struct {
	ID                string `json:"id"`
	CoreID            string `json:"coreId"`
	CoreName          string `json:"coreName"`
	EmulatorJSVersion string `json:"emulatorjsVersion"`
	MaxPlayers        int    `json:"maxPlayers"`
}

type GameSummary struct {
	GameID               string           `json:"gameId"`
	Title                string           `json:"title"`
	CoverURL             *string          `json:"coverUrl"`
	PlatformID           string           `json:"platformId"`
	PlatformName         string           `json:"platformName"`
	PlatformInstanceID   string           `json:"platformInstanceId"`
	PlatformInstanceName string           `json:"platformInstanceName"`
	LastPlayedAtMS       *int64           `json:"lastPlayedAtMs"`
	AddedAtMS            int64            `json:"addedAtMs"`
	Availability         string           `json:"availability"`
	NetplayProfiles      []ProfileSummary `json:"netplayProfiles"`
	BlockerCode          *string          `json:"blockerCode"`
}

type eligibleProfile struct {
	Summary                ProfileSummary
	Manifest               ManifestProfile
	VariantRevisionID      string
	CoreArtifactID         string
	DependencySnapshotJSON string
	DefaultCoreOptions     map[string]string
}

func (service *Service) Games(ctx context.Context, profileID, availability string) ([]GameSummary, error) {
	if availability == "" {
		availability = "SUPPORTED"
	}
	if availability != "SUPPORTED" && availability != "ALL" {
		return nil, ErrInvalidProfile
	}
	allItems, err := service.queryGames(ctx, profileID)
	if err != nil {
		return nil, err
	}
	items := make([]GameSummary, 0, len(allItems))
	for _, item := range allItems {
		item, include, enrichErr := service.enrichGame(ctx, item, availability)
		if enrichErr != nil {
			return nil, enrichErr
		}
		if include {
			items = append(items, item)
		}
	}
	return items, nil
}

func (service *Service) queryGames(ctx context.Context, profileID string) ([]GameSummary, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT game.id,metadata.title,platform.id,platform.name,instance.id,instance.name,game.created_at_ms,
  (SELECT max(play.started_at_ms) FROM play_sessions play WHERE play.game_id=game.id AND play.profile_id=?),
  (SELECT asset.id FROM game_assets asset
   WHERE asset.game_id=game.id AND asset.metadata_revision_id=game.current_metadata_revision_id
     AND asset.kind='COVER' AND asset.ordinal=0 LIMIT 1)
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
WHERE game.status='PUBLISHED' AND instance.enabled=1
ORDER BY lower(metadata.title),game.id
`, profileID)
	if err != nil {
		return nil, fmt.Errorf("netplay/list games: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	allItems := make([]GameSummary, 0)
	for rows.Next() {
		var item GameSummary
		var coverID sql.NullString
		var lastPlayed sql.NullInt64
		if err := rows.Scan(
			&item.GameID, &item.Title, &item.PlatformID, &item.PlatformName,
			&item.PlatformInstanceID, &item.PlatformInstanceName, &item.AddedAtMS, &lastPlayed, &coverID,
		); err != nil {
			return nil, fmt.Errorf("netplay/scan game: %w", err)
		}
		if lastPlayed.Valid {
			item.LastPlayedAtMS = &lastPlayed.Int64
		}
		if coverID.Valid {
			value := "/content/assets/" + coverID.String
			item.CoverURL = &value
		}
		allItems = append(allItems, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/list games: %w", err)
	}
	return allItems, nil
}

func (service *Service) enrichGame(
	ctx context.Context,
	item GameSummary,
	availability string,
) (GameSummary, bool, error) {
	profiles, blocker, err := service.profileEligibility(ctx, item.GameID)
	if err != nil {
		return GameSummary{}, false, err
	}
	item.NetplayProfiles = make([]ProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		item.NetplayProfiles = append(item.NetplayProfiles, profile.Summary)
	}
	if len(item.NetplayProfiles) > 0 {
		item.Availability = "SUPPORTED"
	} else {
		item.Availability = "UNSUPPORTED"
		item.BlockerCode = &blocker
	}
	return item, availability == "ALL" || item.Availability == "SUPPORTED", nil
}

func (service *Service) eligibleProfiles(ctx context.Context, gameID string) ([]eligibleProfile, error) {
	profiles, _, err := service.profileEligibility(ctx, gameID)
	return profiles, err
}

func eligibilityBlocker(hasVariant, contentKindAllowed, coreAllowed bool) string {
	if !hasVariant {
		return "GAME_UNAVAILABLE"
	}
	if !contentKindAllowed {
		return "CONTENT_NOT_ALLOWLISTED"
	}
	if !coreAllowed {
		return "CORE_NOT_ALLOWLISTED"
	}
	return "DEPENDENCY_STALE"
}

func (service *Service) dependencySnapshotCurrent(
	ctx context.Context, artifactID, logicalName, rawSnapshot string,
) (bool, error) {
	lockedJSON, valid := lockedSnapshotJSON(rawSnapshot)
	if !valid {
		return false, nil
	}
	current, status, _, err := corevalidation.ResolveBIOS(ctx, service.database, artifactID, logicalName)
	if err != nil {
		return false, serviceError("resolve BIOS snapshot", err)
	}
	currentJSON, err := current.JSON()
	if err != nil {
		return false, serviceError("serialize BIOS snapshot", err)
	}
	return status == "READY" && bytes.Equal(lockedJSON, currentJSON), nil
}

func lockedSnapshotJSON(raw string) ([]byte, bool) {
	locked, err := corevalidation.ParseSnapshot(raw)
	if err != nil {
		return nil, false
	}
	encoded, err := locked.JSON()
	return encoded, err == nil
}

type eligibilityRow struct {
	revisionID, artifactID, coreID, coreName, emulatorVersion, artifactSHA string
	dependencyJSON, compatibilityJSON, contentKind, logicalName            string
	artifactEnabled                                                        int
}

func (service *Service) profileEligibility(ctx context.Context, gameID string) ([]eligibleProfile, string, error) {
	lockedRows, err := service.queryEligibilityRows(ctx, gameID)
	if err != nil {
		return nil, "", err
	}
	result := make([]eligibleProfile, 0)
	seen := make(map[string]struct{})
	contentKindAllowed, coreAllowed := false, false
	for _, row := range lockedRows {
		for _, candidate := range service.registry.Profiles() {
			if _, duplicate := seen[candidate.ID]; duplicate {
				continue
			}
			profile, contentMatch, coreMatch, current, matchErr := service.matchEligibleProfile(ctx, row, candidate)
			contentKindAllowed = contentKindAllowed || contentMatch
			coreAllowed = coreAllowed || coreMatch
			if matchErr != nil {
				return nil, "", matchErr
			}
			if current {
				result = append(result, profile)
				seen[candidate.ID] = struct{}{}
			}
		}
	}
	slices.SortFunc(result, func(left, right eligibleProfile) int {
		return strings.Compare(left.Summary.ID, right.Summary.ID)
	})
	return result, eligibilityBlocker(len(lockedRows) > 0, contentKindAllowed, coreAllowed), nil
}

func (service *Service) queryEligibilityRows(ctx context.Context, gameID string) ([]eligibilityRow, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT revision.id,artifact.id,artifact.core_id,core.name,artifact.emulatorjs_version,artifact.sha256,
  revision.dependency_snapshot_json,artifact.compatibility_config_json,content.content_kind,
  file.logical_name,artifact.enabled
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
  AND revision.game_content_revision_id=game.current_content_revision_id AND revision.status='READY'
JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
JOIN cores core ON core.id=artifact.core_id
JOIN game_content_revisions content ON content.id=revision.game_content_revision_id
JOIN game_content_files file ON file.game_content_revision_id=content.id AND file.role='CONTENT'
WHERE game.id=? AND game.status='PUBLISHED'
ORDER BY artifact.core_id,revision.id,file.sort_order,file.logical_name
`, gameID)
	if err != nil {
		return nil, fmt.Errorf("netplay/eligible profiles: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	lockedRows := make([]eligibilityRow, 0)
	for rows.Next() {
		var row eligibilityRow
		if err := rows.Scan(
			&row.revisionID, &row.artifactID, &row.coreID, &row.coreName, &row.emulatorVersion, &row.artifactSHA,
			&row.dependencyJSON, &row.compatibilityJSON, &row.contentKind, &row.logicalName, &row.artifactEnabled,
		); err != nil {
			return nil, fmt.Errorf("netplay/eligible profile row: %w", err)
		}
		lockedRows = append(lockedRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/eligible profiles: %w", err)
	}
	return lockedRows, nil
}

func (service *Service) matchEligibleProfile(
	ctx context.Context,
	row eligibilityRow,
	candidate ManifestProfile,
) (eligibleProfile, bool, bool, bool, error) {
	contentKindAllowed, artifactMatches := service.matchesCoreProfile(row, candidate)
	if !contentKindAllowed {
		return eligibleProfile{}, false, false, false, nil
	}
	if !artifactMatches {
		return eligibleProfile{}, contentKindAllowed, false, false, nil
	}
	current, err := service.dependencySnapshotCurrent(ctx, row.artifactID, row.logicalName, row.dependencyJSON)
	if err != nil {
		return eligibleProfile{}, contentKindAllowed, true, false, fmt.Errorf("netplay/dependency snapshot: %w", err)
	}
	return eligibleProfile{
		Summary: ProfileSummary{
			ID: candidate.ID, CoreID: row.coreID, CoreName: row.coreName,
			EmulatorJSVersion: row.emulatorVersion, MaxPlayers: candidate.MaxPlayers,
		},
		Manifest: candidate, VariantRevisionID: row.revisionID, CoreArtifactID: row.artifactID,
		DependencySnapshotJSON: row.dependencyJSON, DefaultCoreOptions: compatibilityOptions(row.compatibilityJSON),
	}, contentKindAllowed, true, current, nil
}

func (service *Service) matchesCoreProfile(row eligibilityRow, candidate ManifestProfile) (bool, bool) {
	contentKindAllowed := slices.Contains(service.registry.Manifest.Protocol.AllowedContentKinds, row.contentKind)
	artifactMatches := contentKindAllowed && row.artifactEnabled == 1 && candidate.CoreID == row.coreID &&
		candidate.EmulatorJSVersion == row.emulatorVersion && candidate.CoreArtifactSHA256 == row.artifactSHA
	return contentKindAllowed, artifactMatches
}

func compatibilityOptions(raw string) map[string]string {
	var compatibility struct {
		DefaultOptions map[string]string `json:"defaultOptions"`
	}
	if json.Unmarshal([]byte(raw), &compatibility) != nil || compatibility.DefaultOptions == nil {
		return map[string]string{}
	}
	return compatibility.DefaultOptions
}

type RoomMember struct {
	MemberID        string `json:"memberId"`
	ProfileID       string `json:"-"`
	PlayerNo        int    `json:"playerNo"`
	Role            string `json:"role"`
	DisplayName     string `json:"displayName"`
	AvatarRef       any    `json:"avatarRef"`
	Ready           bool   `json:"ready"`
	ConnectionState string `json:"connectionState"`
}

type RoomGame struct {
	GameID            string `json:"gameId"`
	Title             string `json:"title"`
	PlatformName      string `json:"platformName"`
	ProfileID         string `json:"profileId"`
	CoreName          string `json:"coreName"`
	EmulatorJSVersion string `json:"emulatorjsVersion"`
	MaxPlayers        int    `json:"maxPlayers"`
}

type SessionSummary struct {
	SessionID string `json:"sessionId"`
	SessionNo int    `json:"sessionNo"`
	State     string `json:"state"`
}

type RoomPermissions struct {
	Host      bool `json:"host"`
	Member    bool `json:"member"`
	CanSelect bool `json:"canSelectGame"`
	CanJoin   bool `json:"canJoin"`
	CanReady  bool `json:"canReady"`
	CanStart  bool `json:"canStart"`
	CanClose  bool `json:"canClose"`
}

type Room struct {
	RoomID         string          `json:"roomId"`
	State          string          `json:"state"`
	Version        int64           `json:"version"`
	Game           *RoomGame       `json:"game"`
	Members        []RoomMember    `json:"members"`
	CurrentSession *SessionSummary `json:"currentSession"`
	Permissions    RoomPermissions `json:"permissions"`
	SelfMemberID   *string         `json:"selfMemberId"`
	ExpiresAtMS    int64           `json:"expiresAtMs"`
	ServerNowMS    int64           `json:"serverNowMs"`
	EndedAtMS      *int64          `json:"endedAtMs"`
	EndReason      *string         `json:"endReason"`
	UpdatedAtMS    int64           `json:"-"`
}

func (service *Service) ListRooms(
	ctx context.Context,
	profileID, view string,
	afterUpdatedAtMS int64,
	afterRoomID string,
	limit int,
) ([]Room, bool, error) {
	if view == "" {
		view = "active"
	}
	if (view != "active" && view != "recent") || limit < 1 || limit > 100 {
		return nil, false, ErrRoomConflict
	}
	query, arguments := service.listRoomsQuery(profileID, view, afterUpdatedAtMS, afterRoomID, limit)
	ids, err := service.queryRoomIDs(ctx, query, arguments, limit)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	result := make([]Room, 0, len(ids))
	for _, roomID := range ids {
		room, err := service.Room(ctx, roomID, profileID)
		if err != nil {
			return nil, false, err
		}
		result = append(result, room)
	}
	return result, hasMore, nil
}

func (service *Service) listRoomsQuery(
	profileID, view string,
	afterUpdatedAtMS int64,
	afterRoomID string,
	limit int,
) (string, []any) {
	terminalClause := "room.state NOT IN ('ENDED','EXPIRED')"
	if view == "recent" {
		terminalClause = "room.state IN ('ENDED','EXPIRED') AND room.ended_at_ms>=?"
	}
	query := `
SELECT DISTINCT room.id
FROM netplay_rooms room
JOIN netplay_room_members member ON member.room_id=room.id
WHERE member.profile_id=? AND ` + terminalClause
	arguments := []any{profileID}
	if view == "recent" {
		arguments = append(arguments, service.clock.Now().Add(-24*time.Hour).UnixMilli())
	}
	if afterRoomID != "" {
		query += ` AND (room.updated_at_ms < ? OR (room.updated_at_ms = ? AND room.id < ?))`
		arguments = append(arguments, afterUpdatedAtMS, afterUpdatedAtMS, afterRoomID)
	}
	query += `
ORDER BY room.updated_at_ms DESC,room.id DESC LIMIT ?
`
	arguments = append(arguments, limit+1)
	return query, arguments
}

func (service *Service) queryRoomIDs(
	ctx context.Context, query string, arguments []any, limit int,
) ([]string, error) {
	rows, err := service.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("netplay/list rooms: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	ids := make([]string, 0, limit+1)
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, fmt.Errorf("netplay/list rooms: %w", err)
		}
		ids = append(ids, roomID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/list rooms: %w", err)
	}
	return ids, nil
}

func (service *Service) CreateRoom(ctx context.Context, profileID string) (Room, error) {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var active int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_rooms WHERE state IN ('DRAFT','WAITING','STARTING','RUNNING')
`).Scan(&active); err != nil {
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	if active >= service.options.MaxActiveRooms {
		return Room{}, ErrCapacity
	}
	roomID, memberID := newV7(), newV7()
	if roomID == "" || memberID == "" {
		return Room{}, errUUIDUnavailable
	}
	expires := now + service.options.DraftIdle.Milliseconds()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_rooms(id,host_profile_id,state,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,'DRAFT',1,?,?,?)
`, roomID, profileID, expires, now, now); err != nil {
		if strings.Contains(err.Error(), "netplay_rooms_one_active_host") {
			return Room{}, ErrRoomConflict
		}
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES(?,?,?,'HOST',1,0,1,?,?)
`, memberID, roomID, profileID, now, now); err != nil {
		return Room{}, fmt.Errorf("netplay/create host: %w", err)
	}
	if err := appendEvent(ctx, transaction, roomID, nil, &profileID, intPointer(1), "ROOM_CREATED", nil, now); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, fmt.Errorf("netplay/create room: %w", err)
	}
	return service.Room(ctx, roomID, profileID)
}

func (service *Service) Room(ctx context.Context, roomID, viewerProfileID string) (Room, error) {
	var result Room
	var gameID, variantID, profileID, digest, sessionID, reason sql.NullString
	var maxPlayers, endedAt sql.NullInt64
	err := service.database.QueryRowContext(ctx, `
SELECT id,state,version,selected_game_id,selected_game_variant_revision_id,netplay_profile_id,
  profile_digest,max_players,current_session_id,expires_at_ms,ended_at_ms,end_reason,updated_at_ms
FROM netplay_rooms WHERE id=?
`, roomID).Scan(
		&result.RoomID, &result.State, &result.Version, &gameID, &variantID, &profileID,
		&digest, &maxPlayers, &sessionID, &result.ExpiresAtMS, &endedAt, &reason, &result.UpdatedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, ErrRoomNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("netplay/get room: %w", err)
	}
	result.ServerNowMS = service.clock.Now().UnixMilli()
	if endedAt.Valid {
		result.EndedAtMS = &endedAt.Int64
	}
	if reason.Valid {
		result.EndReason = &reason.String
	}
	if gameID.Valid {
		var game RoomGame
		game.GameID, game.ProfileID, game.MaxPlayers = gameID.String, profileID.String, int(maxPlayers.Int64)
		if err := service.database.QueryRowContext(ctx, `
SELECT metadata.title,platform.name,core.name,artifact.emulatorjs_version
FROM games game
JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
JOIN platform_instances instance ON instance.id=game.platform_instance_id
JOIN platforms platform ON platform.id=instance.platform_id
JOIN game_variant_revisions revision ON revision.id=?
JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
JOIN cores core ON core.id=artifact.core_id
WHERE game.id=?
`, variantID.String, gameID.String).Scan(
			&game.Title, &game.PlatformName, &game.CoreName, &game.EmulatorJSVersion,
		); err != nil {
			return Room{}, fmt.Errorf("netplay/get room game: %w", err)
		}
		result.Game = &game
	}
	members, err := service.roomMembers(ctx, roomID, sessionID)
	if err != nil {
		return Room{}, err
	}
	result.Members = members
	for _, member := range members {
		if member.ProfileID == viewerProfileID {
			value := member.MemberID
			result.SelfMemberID = &value
			result.Permissions.Member = true
			result.Permissions.Host = member.Role == "HOST"
			break
		}
	}
	if sessionID.Valid {
		var session SessionSummary
		if err := service.database.QueryRowContext(ctx, `
SELECT id,session_no,state FROM netplay_sessions WHERE id=?
`, sessionID.String).Scan(&session.SessionID, &session.SessionNo, &session.State); err != nil {
			return Room{}, fmt.Errorf("netplay/get room session: %w", err)
		}
		result.CurrentSession = &session
	}
	setRoomPermissions(&result)
	return result, nil
}

func setRoomPermissions(room *Room) {
	waiting := room.State == RoomStateWaiting
	room.Permissions.CanSelect = room.Permissions.Host && (room.State == RoomStateDraft || waiting)
	room.Permissions.CanJoin = waiting && !room.Permissions.Member
	room.Permissions.CanReady = waiting && room.Permissions.Member
	room.Permissions.CanStart = waiting && room.Permissions.Host && roomReady(room.Members)
	room.Permissions.CanClose = room.Permissions.Host && room.State != "ENDED" && room.State != "EXPIRED"
}

func (service *Service) roomMembers(
	ctx context.Context, roomID string, sessionID sql.NullString,
) ([]RoomMember, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT member.id,member.profile_id,member.player_no,member.role,profile.display_name,member.ready,
  COALESCE(participant.state,'NOT_CONNECTED')
FROM netplay_room_members member
JOIN profiles profile ON profile.id=member.profile_id
LEFT JOIN netplay_session_participants participant
  ON participant.room_member_id=member.id AND participant.netplay_session_id=?
WHERE member.room_id=? AND member.left_at_ms IS NULL
ORDER BY member.player_no
`, nullableString(sessionID), roomID)
	if err != nil {
		return nil, fmt.Errorf("netplay/list members: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]RoomMember, 0, 4)
	for rows.Next() {
		var member RoomMember
		var ready int
		if err := rows.Scan(
			&member.MemberID, &member.ProfileID, &member.PlayerNo, &member.Role,
			&member.DisplayName, &ready, &member.ConnectionState,
		); err != nil {
			return nil, fmt.Errorf("netplay/scan member: %w", err)
		}
		member.Ready = ready == 1
		result = append(result, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("netplay/list members: %w", err)
	}
	return result, nil
}

func roomReady(members []RoomMember) bool {
	return len(members) >= 2 && !slices.ContainsFunc(members, func(member RoomMember) bool { return !member.Ready })
}

func (service *Service) SelectGame(
	ctx context.Context, roomID, actorProfileID, gameID, profileID string, expectedVersion int64,
) (Room, error) {
	profiles, err := service.eligibleProfiles(ctx, gameID)
	if err != nil {
		return Room{}, fmt.Errorf("netplay/select game profiles: %w", err)
	}
	selected := findEligibleProfile(profiles, profileID)
	if selected == nil {
		return Room{}, ErrInvalidProfile
	}
	canonical, digest, err := service.registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: selected.Manifest, CoreArtifactID: selected.CoreArtifactID,
		GameVariantRevisionID:  selected.VariantRevisionID,
		DependencySnapshotJSON: selected.DependencySnapshotJSON, DefaultCoreOptions: selected.DefaultCoreOptions,
	})
	if err != nil || len(canonical) == 0 {
		return Room{}, ErrInvalidProfile
	}
	if err := service.commitSelectedGame(
		ctx, roomID, actorProfileID, gameID, profileID, digest, selected, expectedVersion,
	); err != nil {
		return Room{}, err
	}
	return service.Room(ctx, roomID, actorProfileID)
}

func (service *Service) commitSelectedGame(
	ctx context.Context,
	roomID, actorProfileID, gameID, profileID, digest string,
	selected *eligibleProfile,
	expectedVersion int64,
) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("select game transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var host string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,host_profile_id,version FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&state, &host, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return serviceError("select game room", err)
	}
	if host != actorProfileID {
		return ErrForbidden
	}
	if version != expectedVersion {
		return ErrPrecondition
	}
	if state != RoomStateDraft && state != RoomStateWaiting {
		return ErrRoomConflict
	}
	var invalidSeat int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_room_members WHERE room_id=? AND left_at_ms IS NULL AND player_no>?
		`, roomID, selected.Manifest.MaxPlayers).Scan(&invalidSeat); err != nil {
		return serviceError("select game seats", err)
	}
	if invalidSeat > 0 {
		return ErrInvalidSeat
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='WAITING',selected_game_id=?,selected_game_variant_revision_id=?,
netplay_profile_id=?,profile_digest=?,max_players=?,current_session_id=NULL,version=version+1,
expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, gameID, selected.VariantRevisionID, profileID, digest, selected.Manifest.MaxPlayers,
		now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
	if err != nil {
		return serviceError("select game update", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPrecondition
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,version=version+1,updated_at_ms=?
WHERE room_id=? AND left_at_ms IS NULL
		`, now, roomID); err != nil {
		return serviceError("select game clear ready", err)
	}
	data := map[string]any{"schemaVersion": 1, "playerCount": selected.Manifest.MaxPlayers}
	if err := appendEvent(
		ctx, transaction, roomID, nil, &actorProfileID, intPointer(1), "GAME_SELECTED", data, now,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("select game commit", err)
	}
	return nil
}

func findEligibleProfile(profiles []eligibleProfile, profileID string) *eligibleProfile {
	for index := range profiles {
		if profiles[index].Manifest.ID == profileID {
			return &profiles[index]
		}
	}
	return nil
}

func (service *Service) ClearGame(
	ctx context.Context,
	roomID, actorProfileID string,
	expectedVersion int64,
) (Room, error) {
	return service.mutateHostRoom(
		ctx, roomID, actorProfileID, expectedVersion, []string{RoomStateWaiting},
		func(transaction *sql.Tx, now int64) error {
			if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='DRAFT',selected_game_id=NULL,selected_game_variant_revision_id=NULL,
netplay_profile_id=NULL,profile_digest=NULL,max_players=NULL,version=version+1,
expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
	`, now+service.options.DraftIdle.Milliseconds(), now, roomID, expectedVersion); err != nil {
				return serviceError("clear game", err)
			}
			if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,version=version+1,updated_at_ms=?
WHERE room_id=? AND left_at_ms IS NULL
`, now, roomID); err != nil {
				return serviceError("clear game ready state", err)
			}
			return appendEvent(ctx, transaction, roomID, nil, &actorProfileID, intPointer(1), "GAME_CLEARED", nil, now)
		},
	)
}

func (service *Service) SetSeat(
	ctx context.Context,
	roomID, profileID string,
	playerNo int,
	expectedVersion int64,
) (Room, error) {
	if playerNo < 2 || playerNo > 4 {
		return Room{}, ErrInvalidSeat
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, serviceError("set seat transaction", err)
	}
	defer cleanup.Rollback(transaction)
	existingID, existingPlayer, err := validateSeatMutation(
		ctx, transaction, roomID, profileID, playerNo, expectedVersion,
	)
	if err != nil {
		return Room{}, err
	}
	eventType, err := persistSeatMember(
		ctx, transaction, roomID, profileID, playerNo, existingID, now,
	)
	if err != nil {
		return Room{}, err
	}
	roomResult, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET version=version+1,expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
	if err != nil {
		return Room{}, serviceError("set seat room version", err)
	}
	if affected, _ := roomResult.RowsAffected(); affected != 1 {
		return Room{}, ErrPrecondition
	}
	data := map[string]any{"schemaVersion": 1, "toPlayerNo": playerNo}
	if existingPlayer.Valid {
		data["fromPlayerNo"] = existingPlayer.Int64
	}
	if err := appendEvent(ctx, transaction, roomID, nil, &profileID, &playerNo, eventType, data, now); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, serviceError("set seat commit", err)
	}
	return service.Room(ctx, roomID, profileID)
}

func validateSeatMutation(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, profileID string,
	playerNo int,
	expectedVersion int64,
) (sql.NullString, sql.NullInt64, error) {
	var state string
	var version, maxPlayers int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version,max_players FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&state, &version, &maxPlayers); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.NullString{}, sql.NullInt64{}, ErrRoomNotFound
		}
		return sql.NullString{}, sql.NullInt64{}, serviceError("set seat room", err)
	}
	if version != expectedVersion {
		return sql.NullString{}, sql.NullInt64{}, ErrPrecondition
	}
	if state != RoomStateWaiting {
		return sql.NullString{}, sql.NullInt64{}, ErrRoomConflict
	}
	if int64(playerNo) > maxPlayers {
		return sql.NullString{}, sql.NullInt64{}, ErrInvalidSeat
	}
	var existingID sql.NullString
	var existingPlayer sql.NullInt64
	var ready sql.NullInt64
	var role sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT id,player_no,ready,role FROM netplay_room_members WHERE room_id=? AND profile_id=?
	`, roomID, profileID).Scan(&existingID, &existingPlayer, &ready, &role)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, sql.NullInt64{}, serviceError("set seat member", err)
	}
	if err := validateSeatMember(role, ready); err != nil {
		return sql.NullString{}, sql.NullInt64{}, err
	}
	var occupied int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_room_members WHERE room_id=? AND player_no=? AND left_at_ms IS NULL
  AND profile_id!=?
		`, roomID, playerNo, profileID).Scan(&occupied); err != nil {
		return sql.NullString{}, sql.NullInt64{}, serviceError("set seat occupancy", err)
	}
	if occupied > 0 {
		return sql.NullString{}, sql.NullInt64{}, ErrSeatTaken
	}
	return existingID, existingPlayer, nil
}

func validateSeatMember(role sql.NullString, ready sql.NullInt64) error {
	if ready.Valid && ready.Int64 == 1 {
		return ErrRoomConflict
	}
	if role.Valid && role.String == "HOST" {
		return ErrForbidden
	}
	return nil
}

func persistSeatMember(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, profileID string,
	playerNo int,
	existingID sql.NullString,
	now int64,
) (string, error) {
	eventType := "SEAT_CHANGED"
	if existingID.Valid {
		if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET player_no=?,ready=0,left_at_ms=NULL,leave_reason=NULL,
version=version+1,updated_at_ms=? WHERE id=?
		`, playerNo, now, existingID.String); err != nil {
			return "", serviceError("change seat", err)
		}
	} else {
		eventType = "MEMBER_JOINED"
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_room_members(id,room_id,profile_id,role,player_no,ready,version,joined_at_ms,updated_at_ms)
VALUES(?,?,?,'GUEST',?,0,1,?,?)
		`, newV7(), roomID, profileID, playerNo, now, now); err != nil {
			return "", serviceError("claim seat", err)
		}
	}
	return eventType, nil
}

func (service *Service) SetReady(
	ctx context.Context,
	roomID, profileID string,
	ready bool,
	expectedVersion int64,
) (Room, error) {
	if err := service.validateReadySelection(ctx, roomID, ready, expectedVersion); err != nil {
		return Room{}, err
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, serviceError("set ready transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var state string
	var version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT state,version FROM netplay_rooms WHERE id=?
`, roomID).Scan(&state, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Room{}, ErrRoomNotFound
		}
		return Room{}, serviceError("set ready room", err)
	}
	if version != expectedVersion {
		return Room{}, ErrPrecondition
	}
	if state != RoomStateWaiting {
		return Room{}, ErrRoomConflict
	}
	readyValue := boolInt(ready)
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=?,version=version+1,updated_at_ms=?
WHERE room_id=? AND profile_id=? AND left_at_ms IS NULL
`, readyValue, now, roomID, profileID)
	if err != nil {
		return Room{}, serviceError("set ready member", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Room{}, ErrForbidden
	}
	roomResult, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET version=version+1,expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
	if err != nil {
		return Room{}, serviceError("set ready room version", err)
	}
	if affected, _ := roomResult.RowsAffected(); affected != 1 {
		return Room{}, ErrPrecondition
	}
	data := map[string]any{"schemaVersion": 1, "ready": ready}
	if err := appendEvent(ctx, transaction, roomID, nil, &profileID, nil, "READY_CHANGED", data, now); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, serviceError("set ready commit", err)
	}
	return service.Room(ctx, roomID, profileID)
}

func (service *Service) validateReadySelection(
	ctx context.Context, roomID string, ready bool, expectedVersion int64,
) error {
	if !ready {
		return nil
	}
	var state, gameID, profileID string
	var version int64
	if err := service.database.QueryRowContext(ctx, `
SELECT state,version,selected_game_id,netplay_profile_id FROM netplay_rooms WHERE id=?
`, roomID).Scan(&state, &version, &gameID, &profileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return serviceError("prevalidate ready room", err)
	}
	if version != expectedVersion {
		return ErrPrecondition
	}
	if state != RoomStateWaiting {
		return ErrRoomConflict
	}
	profiles, err := service.eligibleProfiles(ctx, gameID)
	if err != nil {
		return fmt.Errorf("netplay/set ready profiles: %w", err)
	}
	if findEligibleProfile(profiles, profileID) == nil {
		return ErrProfileStale
	}
	return nil
}

func (service *Service) Start(ctx context.Context, roomID, hostProfileID string, expectedVersion int64) (Room, error) {
	prevalidated, err := loadStartRoom(ctx, service.database, roomID, hostProfileID, expectedVersion)
	if err != nil {
		return Room{}, err
	}
	locked, canonical, digest, err := service.lockedStartProfile(ctx, prevalidated)
	if err != nil {
		return Room{}, err
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, serviceError("start transaction", err)
	}
	defer cleanup.Rollback(transaction)
	state, err := loadStartRoom(ctx, transaction, roomID, hostProfileID, expectedVersion)
	if err != nil {
		return Room{}, err
	}
	if state != prevalidated {
		return Room{}, ErrPrecondition
	}
	members, mask, err := loadLockedMembers(ctx, transaction, roomID, state.maxPlayers)
	if err != nil {
		return Room{}, err
	}
	sessionID, err := createNetplaySession(
		ctx, transaction, roomID, state, locked, canonical, digest, members, mask, now,
	)
	if err != nil {
		return Room{}, err
	}
	roomResult, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='STARTING',current_session_id=?,version=version+1,updated_at_ms=?
WHERE id=? AND version=?
`, sessionID, now, roomID, expectedVersion)
	if err != nil {
		return Room{}, serviceError("start room state", err)
	}
	if affected, _ := roomResult.RowsAffected(); affected != 1 {
		return Room{}, ErrPrecondition
	}
	data := map[string]any{
		"schemaVersion": 1, "playerCount": len(members), "occupiedSeatMask": mask,
	}
	if err := appendEvent(
		ctx, transaction, roomID, &sessionID, &hostProfileID, intPointer(1), "SESSION_CREATED", data, now,
	); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, serviceError("start commit", err)
	}
	return service.Room(ctx, roomID, hostProfileID)
}

type startRoomState struct {
	gameID        string
	revisionID    string
	profileID     string
	profileDigest string
	maxPlayers    int64
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadStartRoom(
	ctx context.Context,
	queryer rowQueryer,
	roomID, hostProfileID string,
	expectedVersion int64,
) (startRoomState, error) {
	var result startRoomState
	var state, host string
	var version int64
	if err := queryer.QueryRowContext(ctx, `
SELECT state,host_profile_id,version,selected_game_id,selected_game_variant_revision_id,
netplay_profile_id,profile_digest,max_players FROM netplay_rooms WHERE id=?
`, roomID).Scan(
		&state, &host, &version, &result.gameID, &result.revisionID,
		&result.profileID, &result.profileDigest, &result.maxPlayers,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return startRoomState{}, ErrRoomNotFound
		}
		return startRoomState{}, serviceError("start room", err)
	}
	if host != hostProfileID {
		return startRoomState{}, ErrForbidden
	}
	if version != expectedVersion {
		return startRoomState{}, ErrPrecondition
	}
	if state != RoomStateWaiting {
		return startRoomState{}, ErrRoomConflict
	}
	return result, nil
}

func (service *Service) lockedStartProfile(
	ctx context.Context, state startRoomState,
) (*eligibleProfile, []byte, string, error) {
	eligible, err := service.eligibleProfiles(ctx, state.gameID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("netplay/start profiles: %w", err)
	}
	var locked *eligibleProfile
	for index := range eligible {
		if eligible[index].Manifest.ID == state.profileID && eligible[index].VariantRevisionID == state.revisionID {
			locked = &eligible[index]
		}
	}
	if locked == nil {
		return nil, nil, "", ErrProfileStale
	}
	canonical, digest, err := service.registry.CanonicalProfile(CanonicalProfileInput{
		ManifestProfile: locked.Manifest, CoreArtifactID: locked.CoreArtifactID,
		GameVariantRevisionID:  locked.VariantRevisionID,
		DependencySnapshotJSON: locked.DependencySnapshotJSON, DefaultCoreOptions: locked.DefaultCoreOptions,
	})
	if err != nil || digest != state.profileDigest {
		return nil, nil, "", ErrProfileStale
	}
	return locked, canonical, digest, nil
}

type lockedMember struct {
	id      string
	profile string
	player  int
}

func loadLockedMembers(
	ctx context.Context, transaction *sql.Tx, roomID string, maxPlayers int64,
) ([]lockedMember, int, error) {
	memberRows, err := transaction.QueryContext(ctx, `
SELECT id,profile_id,player_no,ready FROM netplay_room_members
WHERE room_id=? AND left_at_ms IS NULL ORDER BY player_no
`, roomID)
	if err != nil {
		return nil, 0, serviceError("start members", err)
	}
	defer func() { cleanup.Error("close", memberRows.Close()) }()
	members := make([]lockedMember, 0, 4)
	mask := 0
	allReady := true
	for memberRows.Next() {
		var member lockedMember
		var ready int
		if err := memberRows.Scan(&member.id, &member.profile, &member.player, &ready); err != nil {
			return nil, 0, serviceError("start member row", err)
		}
		allReady = allReady && ready == 1
		mask |= 1 << (member.player - 1)
		members = append(members, member)
	}
	if err := memberRows.Err(); err != nil {
		return nil, 0, serviceError("start members", err)
	}
	if len(members) < 2 || len(members) > int(maxPlayers) || !allReady {
		return nil, 0, ErrRoomNotReady
	}
	return members, mask, nil
}

func createNetplaySession(
	ctx context.Context,
	transaction *sql.Tx,
	roomID string,
	state startRoomState,
	locked *eligibleProfile,
	canonical []byte,
	digest string,
	members []lockedMember,
	mask int,
	now int64,
) (string, error) {
	var sessionNo int
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(max(session_no),0)+1 FROM netplay_sessions WHERE room_id=?
`, roomID).Scan(&sessionNo); err != nil {
		return "", serviceError("start session number", err)
	}
	sessionID := newV7()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_sessions(id,room_id,session_no,state,game_id,game_variant_revision_id,
core_artifact_id,netplay_profile_id,profile_json,profile_digest,player_count,occupied_seat_mask,
authority_player_no,resync_count,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,'PREPARING',?,?,?,?,?,?,?, ?,1,0,1,?,?)
`, sessionID, roomID, sessionNo, state.gameID, state.revisionID, locked.CoreArtifactID, state.profileID,
		string(canonical), digest, len(members), mask, now, now); err != nil {
		return "", fmt.Errorf("netplay/create session: %w", err)
	}
	for _, member := range members {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_session_participants(netplay_session_id,profile_id,room_member_id,player_no,
state,credential_generation,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,'LOCKED',0,1,?,?)
	`, sessionID, member.profile, member.id, member.player, now, now); err != nil {
			return "", serviceError("start participant", err)
		}
	}
	return sessionID, nil
}

func (service *Service) EndRoom(
	ctx context.Context,
	roomID, actorProfileID, reason string,
	expectedVersion *int64,
) error {
	return service.endRoom(ctx, roomID, actorProfileID, reason, expectedVersion, true)
}

func (service *Service) endRoomSystem(ctx context.Context, roomID, reason string) error {
	return service.endRoom(ctx, roomID, "", reason, nil, false)
}

func (service *Service) endRoom(
	ctx context.Context, roomID, actorProfileID, reason string, expectedVersion *int64, authorize bool,
) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("end room transaction", err)
	}
	defer cleanup.Rollback(transaction)
	state, err := service.loadEndRoomState(ctx, transaction, roomID, actorProfileID, authorize)
	if err != nil {
		return err
	}
	if expectedVersion != nil && state.version != *expectedVersion {
		return ErrPrecondition
	}
	if state.terminal {
		return nil
	}
	reason = normalizeEndReason(reason, state.actorIsHost)
	disposition := endDisposition(reason, state.actorIsHost)
	sessionState := endSessionState(reason)
	if state.sessionID.Valid {
		if err := closeNetplaySession(ctx, transaction, state.sessionID.String, sessionState, reason, now); err != nil {
			return err
		}
	}
	if err := service.applyEndDisposition(
		ctx, transaction, roomID, actorProfileID, reason, sessionState, disposition, state.sessionID, now,
	); err != nil {
		return err
	}
	var actor *string
	if authorize {
		actor = &actorProfileID
	}
	if err := appendRoomEndedEvent(
		ctx, transaction, roomID, reason, disposition, state.sessionID, actor, now,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("end room commit", err)
	}
	return nil
}

func normalizeEndReason(reason string, actorIsHost bool) string {
	if reason == "PEER_TIMEOUT" && actorIsHost {
		return "HOST_LOST"
	}
	return reason
}

func (service *Service) applyEndDisposition(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, actorProfileID, reason, sessionState, disposition string,
	sessionID sql.NullString,
	now int64,
) error {
	if disposition == RoomDispositionWaiting {
		return service.returnRoomToWaiting(
			ctx, transaction, roomID, actorProfileID, reason, sessionState, sessionID, now,
		)
	}
	return endRoomRecord(ctx, transaction, roomID, reason, now)
}

func appendRoomEndedEvent(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, reason, disposition string,
	sessionID sql.NullString,
	actor *string,
	now int64,
) error {
	if disposition != RoomDispositionEnded {
		return nil
	}
	data := map[string]any{"schemaVersion": 1, "reason": reason}
	return appendEvent(
		ctx, transaction, roomID, stringPointerFromNull(sessionID), actor, nil, "ROOM_ENDED", data, now,
	)
}

type endRoomState struct {
	sessionID   sql.NullString
	version     int64
	actorIsHost bool
	terminal    bool
}

func (service *Service) loadEndRoomState(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, actorProfileID string,
	authorize bool,
) (endRoomState, error) {
	var state, host string
	var result endRoomState
	err := transaction.QueryRowContext(ctx, `
SELECT state,host_profile_id,version,current_session_id FROM netplay_rooms WHERE id=?
`, roomID).Scan(&state, &host, &result.version, &result.sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return endRoomState{}, ErrRoomNotFound
	}
	if err != nil {
		return endRoomState{}, serviceError("end room state", err)
	}
	result.actorIsHost = actorProfileID != "" && actorProfileID == host
	result.terminal = state == "ENDED" || state == "EXPIRED"
	if !authorize || actorProfileID == host {
		return result, nil
	}
	var member int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_room_members
WHERE room_id=? AND profile_id=? AND left_at_ms IS NULL
`, roomID, actorProfileID).Scan(&member); err != nil {
		return endRoomState{}, serviceError("authorize room end", err)
	}
	if member != 1 {
		return endRoomState{}, ErrForbidden
	}
	return result, nil
}

func endSessionState(reason string) string {
	if reason == "NORMAL" || reason == "USER_EXIT" {
		return "FINISHED"
	}
	return "FAILED"
}

func closeNetplaySession(
	ctx context.Context,
	transaction *sql.Tx,
	sessionID, sessionState, reason string,
	now int64,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state=?,finished_at_ms=?,end_reason=?,version=version+1,updated_at_ms=?
WHERE id=? AND state NOT IN ('FINISHED','FAILED')
`, sessionState, now, reason, now, sessionID); err != nil {
		return serviceError("finish session", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE netplay_session_id=? AND state IN ('CREATED','ACTIVE')
`, now, now, sessionID); err != nil {
		return serviceError("revoke session launches", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='LEFT',disconnected_at_ms=NULL,lease_expires_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE netplay_session_id=? AND state!='LEFT'
`, now, sessionID); err != nil {
		return serviceError("close session participants", err)
	}
	return nil
}

func (service *Service) returnRoomToWaiting(
	ctx context.Context,
	transaction *sql.Tx,
	roomID, actorProfileID, reason, sessionState string,
	sessionID sql.NullString,
	now int64,
) error {
	if actorProfileID != "" && guestLeavesAfterSession(reason) {
		leaveReason := guestLeaveReason(reason)
		if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,left_at_ms=?,leave_reason=?,version=version+1,updated_at_ms=?
WHERE room_id=? AND profile_id=? AND role='GUEST' AND left_at_ms IS NULL
`, now, leaveReason, now, roomID, actorProfileID); err != nil {
			return serviceError("release guest after session", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,version=version+1,updated_at_ms=?
WHERE room_id=? AND left_at_ms IS NULL
`, now, roomID); err != nil {
		return serviceError("clear room ready state", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='WAITING',current_session_id=NULL,version=version+1,
expires_at_ms=?,updated_at_ms=? WHERE id=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID); err != nil {
		return serviceError("return room to waiting", err)
	}
	if !sessionID.Valid {
		return nil
	}
	data := map[string]any{"schemaVersion": 1, "toState": sessionState, "reason": reason}
	return appendEvent(
		ctx, transaction, roomID, stringPointerFromNull(sessionID), nil, nil, "SESSION_STATE_CHANGED", data, now,
	)
}

func guestLeavesAfterSession(reason string) bool {
	return slices.Contains([]string{
		"USER_EXIT", "PEER_TIMEOUT", "AUTH_REVOKED", "PROTOCOL_VIOLATION", "PEER_TOO_SLOW",
	}, reason)
}

func guestLeaveReason(reason string) string {
	switch reason {
	case "USER_EXIT":
		return "USER_LEFT"
	case "AUTH_REVOKED":
		return "AUTH_REVOKED"
	default:
		return "SESSION_ENDED"
	}
}

func endRoomRecord(ctx context.Context, transaction *sql.Tx, roomID, reason string, now int64) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,left_at_ms=?,leave_reason='ROOM_ENDED',version=version+1,updated_at_ms=?
WHERE room_id=? AND left_at_ms IS NULL
`, now, now, roomID); err != nil {
		return serviceError("end room members", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='ENDED',current_session_id=NULL,ended_at_ms=?,end_reason=?,
version=version+1,updated_at_ms=? WHERE id=?
`, now, reason, now, roomID); err != nil {
		return serviceError("end room", err)
	}
	return nil
}

func (service *Service) Leave(ctx context.Context, roomID, profileID string, expectedVersion int64) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("leave room transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var state, role string
	var version int64
	var memberID string
	if err := transaction.QueryRowContext(ctx, `
SELECT room.state,room.version,member.id,member.role
FROM netplay_rooms room JOIN netplay_room_members member ON member.room_id=room.id
WHERE room.id=? AND member.profile_id=? AND member.left_at_ms IS NULL
`, roomID, profileID).Scan(&state, &version, &memberID, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return serviceError("leave room member", err)
	}
	if role == "HOST" {
		return ErrForbidden
	}
	if version != expectedVersion {
		return ErrPrecondition
	}
	if state == RoomStateStarting || state == RoomStateRunning {
		cleanup.Rollback(transaction)
		return service.EndRoom(ctx, roomID, profileID, "USER_EXIT", nil)
	}
	if state != RoomStateWaiting {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,left_at_ms=?,leave_reason='USER_LEFT',version=version+1,updated_at_ms=?
WHERE id=?
	`, now, now, memberID); err != nil {
		return serviceError("leave room member update", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET version=version+1,expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
	if err != nil {
		return serviceError("leave room version", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPrecondition
	}
	if err := appendEvent(ctx, transaction, roomID, nil, &profileID, nil, "MEMBER_LEFT", nil, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("leave room commit", err)
	}
	return nil
}

func (service *Service) Kick(
	ctx context.Context, roomID, hostProfileID, memberID string, expectedVersion int64,
) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("kick member transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var state, host string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,host_profile_id,version FROM netplay_rooms WHERE id=?`, roomID,
	).
		Scan(&state, &host, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRoomNotFound
		}
		return serviceError("kick member room", err)
	}
	if host != hostProfileID {
		return ErrForbidden
	}
	if version != expectedVersion {
		return ErrPrecondition
	}
	if state != RoomStateWaiting {
		return ErrRoomConflict
	}
	var targetProfile string
	var playerNo int
	if err := transaction.QueryRowContext(ctx, `
SELECT profile_id,player_no FROM netplay_room_members
WHERE id=? AND room_id=? AND role='GUEST' AND left_at_ms IS NULL
`, memberID, roomID).Scan(&targetProfile, &playerNo); err != nil {
		return ErrForbidden
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_room_members SET ready=0,left_at_ms=?,leave_reason='HOST_KICKED',version=version+1,updated_at_ms=?
WHERE id=?
	`, now, now, memberID); err != nil {
		return serviceError("kick member update", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET version=version+1,expires_at_ms=?,updated_at_ms=? WHERE id=? AND version=?
`, now+service.options.WaitingIdle.Milliseconds(), now, roomID, expectedVersion)
	if err != nil {
		return serviceError("kick member room version", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrPrecondition
	}
	if err := appendEvent(
		ctx, transaction, roomID, nil, &targetProfile, &playerNo, "MEMBER_KICKED", nil, now,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("kick member commit", err)
	}
	return nil
}

func (service *Service) SetSessionState(
	ctx context.Context, roomID, sessionID, profileID, target string,
) error {
	allowed := target == "PAUSED_RECONNECT" || target == "RUNNING"
	if !allowed {
		return ErrRoomConflict
	}
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("set session state transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var state, host string
	if err := transaction.QueryRowContext(ctx, `
SELECT session.state,room.host_profile_id FROM netplay_sessions session
JOIN netplay_rooms room ON room.id=session.room_id
WHERE session.id=? AND session.room_id=?
`, sessionID, roomID).Scan(&state, &host); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSessionNotFound
		}
		return serviceError("set session state read", err)
	}
	if host != profileID {
		return ErrForbidden
	}
	if target == "PAUSED_RECONNECT" && state != "RUNNING" || target == "RUNNING" && state != "PAUSED_RECONNECT" {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state=?,version=version+1,updated_at_ms=? WHERE id=?
	`, target, now, sessionID); err != nil {
		return serviceError("set session state update", err)
	}
	eventType := "PAUSED"
	if target == "RUNNING" {
		eventType = "RESUMED"
	}
	if err := appendEvent(ctx, transaction, roomID, &sessionID, &profileID, intPointer(1), eventType,
		map[string]any{"schemaVersion": 1, "fromState": state, "toState": target}, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("set session state commit", err)
	}
	return nil
}

func (service *Service) PrepareResync(ctx context.Context, roomID, sessionID string) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("prepare resync transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var fromState string
	if err := transaction.QueryRowContext(
		ctx, `SELECT state FROM netplay_sessions WHERE id=? AND room_id=?`, sessionID, roomID,
	).Scan(&fromState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSessionNotFound
		}
		return serviceError("prepare resync state", err)
	}
	if fromState != "PAUSED_RECONNECT" && fromState != "RUNNING" {
		return ErrRoomConflict
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='RESYNCHRONIZING',resync_count=resync_count+1,
version=version+1,updated_at_ms=? WHERE id=? AND room_id=? AND state=?
`, now, sessionID, roomID, fromState)
	if err != nil {
		return serviceError("prepare resync session", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='RUNTIME_READY',disconnected_at_ms=NULL,
lease_expires_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE netplay_session_id=? AND state IN ('CONNECTED','DISCONNECTED')
	`, now, sessionID); err != nil {
		return serviceError("prepare resync participants", err)
	}
	eventType := "RESUMED"
	reason := "PEER_RECONNECTED"
	if fromState == "RUNNING" {
		eventType = "PAUSED"
		reason = "STATE_MISMATCH"
	}
	data := map[string]any{
		"schemaVersion": 1, "fromState": fromState, "toState": "RESYNCHRONIZING", "reason": reason,
	}
	if err := appendEvent(
		ctx, transaction, roomID, &sessionID, nil, nil, eventType, data, now,
	); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("prepare resync commit", err)
	}
	return nil
}

func (service *Service) MarkDisconnected(ctx context.Context, participant SocketParticipant) error {
	now := service.clock.Now().UnixMilli()
	leaseExpires := now + service.options.ReconnectLease.Milliseconds()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("mark disconnected transaction", err)
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='DISCONNECTED',disconnected_at_ms=?,lease_expires_at_ms=?,
version=version+1,updated_at_ms=? WHERE netplay_session_id=? AND profile_id=? AND state='CONNECTED'
	`, now, leaseExpires, now, participant.SessionID, participant.ProfileID); err != nil {
		return serviceError("mark disconnected participant", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='PAUSED_RECONNECT',version=version+1,updated_at_ms=?
WHERE id=? AND room_id=? AND state='RUNNING'
`, now, participant.SessionID, participant.RoomID)
	if err != nil {
		return serviceError("pause disconnected session", err)
	}
	if affected, _ := result.RowsAffected(); affected == 1 {
		if err := appendEvent(ctx, transaction, participant.RoomID, &participant.SessionID,
			&participant.ProfileID, &participant.PlayerNo, "PAUSED", map[string]any{
				"schemaVersion": 1, "fromState": "RUNNING", "toState": "PAUSED_RECONNECT",
				"reason": "PEER_DISCONNECTED",
			}, now); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("mark disconnected commit", err)
	}
	return nil
}

type Event struct {
	ID        int64          `json:"id"`
	EventType string         `json:"eventType"`
	Data      map[string]any `json:"data"`
	CreatedAt int64          `json:"createdAtMs"`
}

type ParticipantLaunch struct {
	Launch           launch.Created
	RoomCapability   string
	CredentialExpiry int64
}

type SocketParticipant struct {
	RoomID               string
	SessionID            string
	ProfileID            string
	PlayerNo             int
	CredentialGeneration int64
	ProfileDigest        string
	CoreArtifactID       string
	RoomVersion          int64
	SessionVersion       int64
	SessionState         string
	OccupiedSeatMask     int
	PlayerCount          int
}

func (service *Service) AuthenticateSocket(
	ctx context.Context, roomID, profileID, encodedCredential string,
) (SocketParticipant, error) {
	var participant SocketParticipant
	var credentialHash []byte
	var launchState string
	err := service.database.QueryRowContext(ctx, `
SELECT room.id,session.id,participant.profile_id,participant.player_no,
  participant.credential_generation,session.profile_digest,session.core_artifact_id,
  room.version,session.version,session.state,session.occupied_seat_mask,session.player_count,
  participant.credential_sha256,launch.state
FROM netplay_rooms room
JOIN netplay_sessions session ON session.id=room.current_session_id AND session.room_id=room.id
JOIN netplay_session_participants participant ON participant.netplay_session_id=session.id
JOIN launch_sessions launch ON launch.id=participant.launch_session_id
WHERE room.id=? AND participant.profile_id=? AND room.state IN ('STARTING','RUNNING')
  AND session.state NOT IN ('FINISHED','FAILED')
`, roomID, profileID).Scan(
		&participant.RoomID, &participant.SessionID, &participant.ProfileID, &participant.PlayerNo,
		&participant.CredentialGeneration, &participant.ProfileDigest, &participant.CoreArtifactID,
		&participant.RoomVersion, &participant.SessionVersion, &participant.SessionState,
		&participant.OccupiedSeatMask, &participant.PlayerCount,
		&credentialHash, &launchState,
	)
	if err != nil || launchState != "ACTIVE" || !MatchesCapability(encodedCredential, credentialHash) {
		return SocketParticipant{}, ErrForbidden
	}
	return participant, nil
}

func (service *Service) MarkRuntimeReady(ctx context.Context, participant SocketParticipant) (bool, error) {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false, serviceError("mark runtime ready transaction", err)
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='RUNTIME_READY',version=version+1,updated_at_ms=?
WHERE netplay_session_id=? AND profile_id=? AND state='LAUNCH_READY'
`, now, participant.SessionID, participant.ProfileID)
	if err != nil {
		return false, serviceError("mark runtime ready participant", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		if err := appendEvent(ctx, transaction, participant.RoomID, &participant.SessionID,
			&participant.ProfileID, &participant.PlayerNo, "PARTICIPANT_STATE_CHANGED", map[string]any{
				"schemaVersion": 1, "fromState": "LAUNCH_READY", "toState": "RUNTIME_READY",
			}, now); err != nil {
			return false, err
		}
	}
	var remaining int
	var sessionState string
	if err := transaction.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM netplay_session_participants
   WHERE netplay_session_id=? AND state!='RUNTIME_READY'),
  (SELECT state FROM netplay_sessions WHERE id=? AND room_id=? )
	`, participant.SessionID, participant.SessionID, participant.RoomID).Scan(&remaining, &sessionState); err != nil {
		return false, serviceError("mark runtime ready remaining", err)
	}
	allReady := remaining == 0 && sessionState == "LOADING"
	if allReady {
		if err := advanceSessionToSynchronizing(ctx, transaction, participant, now); err != nil {
			return false, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, serviceError("mark runtime ready commit", err)
	}
	return allReady, nil
}

func advanceSessionToSynchronizing(
	ctx context.Context, transaction *sql.Tx, participant SocketParticipant, now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='SYNCHRONIZING',version=version+1,updated_at_ms=?
WHERE id=? AND state='LOADING'
`, now, participant.SessionID)
	if err != nil {
		return serviceError("mark runtime ready session", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil
	}
	data := map[string]any{
		"schemaVersion": 1, "fromState": "LOADING", "toState": "SYNCHRONIZING",
	}
	return appendEvent(
		ctx, transaction, participant.RoomID, &participant.SessionID, nil, nil,
		"SESSION_STATE_CHANGED", data, now,
	)
}

func (service *Service) MarkSessionRunning(ctx context.Context, roomID, sessionID string) error {
	now := service.clock.Now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("mark session running transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var fromState string
	var resyncCount int
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,resync_count FROM netplay_sessions WHERE id=? AND room_id=?`, sessionID, roomID,
	).
		Scan(&fromState, &resyncCount); err != nil {
		return serviceError("mark session running state", err)
	}
	if fromState != "SYNCHRONIZING" && fromState != "RESYNCHRONIZING" {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_session_participants SET state='CONNECTED',version=version+1,updated_at_ms=?
WHERE netplay_session_id=? AND state IN ('RUNTIME_READY','SYNCHRONIZED')
	`, now, sessionID); err != nil {
		return serviceError("mark session participants connected", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='RUNNING',started_at_ms=COALESCE(started_at_ms,?),version=version+1,updated_at_ms=?
WHERE id=? AND room_id=? AND state IN ('SYNCHRONIZING','RESYNCHRONIZING')
`, now, now, sessionID, roomID)
	if err != nil {
		return serviceError("mark session running", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrRoomConflict
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='RUNNING',version=version+1,updated_at_ms=?
WHERE id=? AND current_session_id=? AND state='STARTING'
	`, now, roomID, sessionID); err != nil {
		return serviceError("mark room running", err)
	}
	if err := appendEvent(ctx, transaction, roomID, &sessionID, nil, nil, "SESSION_STATE_CHANGED",
		map[string]any{"schemaVersion": 1, "fromState": fromState, "toState": "RUNNING"}, now); err != nil {
		return err
	}
	if fromState == "RESYNCHRONIZING" {
		if err := appendEvent(ctx, transaction, roomID, &sessionID, nil, nil, "RESYNCED",
			map[string]any{"schemaVersion": 1, "resyncCount": resyncCount}, now); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("mark session running commit", err)
	}
	return nil
}

func (service *Service) ParticipantCapability(
	ctx context.Context, sessionID, profileID string,
) (string, error) {
	var generation int64
	var credentialHash []byte
	var launchID string
	if err := service.database.QueryRowContext(ctx, `
SELECT credential_generation,credential_sha256,launch_session_id
FROM netplay_session_participants
WHERE netplay_session_id=? AND profile_id=? AND launch_session_id IS NOT NULL
`, sessionID, profileID).Scan(&generation, &credentialHash, &launchID); err != nil {
		return "", ErrForbidden
	}
	sessionUUID, sessionErr := uuid.Parse(sessionID)
	profileUUID, profileErr := uuid.Parse(profileID)
	if sessionErr != nil || profileErr != nil || generation < 1 {
		return "", ErrForbidden
	}
	credential := service.credentials.Capability(sessionUUID, profileUUID, uint32(generation))
	if !MatchesCapability(EncodeCapability(credential), credentialHash) {
		return "", ErrForbidden
	}
	return EncodeCapability(credential), nil
}

func (service *Service) failPreparation(ctx context.Context, roomID string, cause error) error {
	rollbackContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := service.endRoomSystem(rollbackContext, roomID, "PREPARE_FAILED"); err != nil {
		return errors.Join(cause, fmt.Errorf("netplay/abort preparation: %w", err))
	}
	return cause
}

func (service *Service) CreateParticipantLaunch(
	ctx context.Context,
	launcher *launch.Service,
	roomID, sessionID, profileID string,
	capabilities launch.Capabilities,
) (ParticipantLaunch, error) {
	service.launchMu.Lock()
	defer service.launchMu.Unlock()
	spec, err := service.participantLaunchSpec(ctx, roomID, sessionID, profileID)
	if err != nil {
		return ParticipantLaunch{}, err
	}
	credential, err := service.participantCredential(sessionID, profileID, spec.generation)
	if err != nil {
		return ParticipantLaunch{}, err
	}
	credentialHash := HashCapability(credential)
	created, err := launcher.CreateNetplay(ctx, launch.NetplayCreateRequest{
		RoomID: roomID, SessionID: sessionID, ProfileID: profileID, PlayerNo: spec.playerNo,
		GameID: spec.gameID, GameVariantRevisionID: spec.revisionID, CoreArtifactID: spec.artifactID,
		ReturnTo: "/netplay/rooms/" + roomID, ClientCapabilities: capabilities,
		CredentialGeneration: spec.generation, NetplayCredentialSHA256: credentialHash[:],
	})
	if err != nil {
		cause := fmt.Errorf("netplay/create participant launch: %w", err)
		return ParticipantLaunch{}, service.failPreparation(ctx, roomID, cause)
	}
	if err := service.recordParticipantLaunch(
		ctx, roomID, sessionID, profileID, spec.playerNo, service.clock.Now().UnixMilli(),
	); err != nil {
		cause := fmt.Errorf("netplay/record participant launch: %w", err)
		return ParticipantLaunch{}, service.failPreparation(ctx, roomID, cause)
	}
	return ParticipantLaunch{
		Launch: created, RoomCapability: EncodeCapability(credential),
		CredentialExpiry: service.clock.Now().Add(8 * time.Hour).UnixMilli(),
	}, nil
}

type participantLaunchSpec struct {
	gameID     string
	revisionID string
	artifactID string
	playerNo   int
	generation int64
}

func (service *Service) participantLaunchSpec(
	ctx context.Context, roomID, sessionID, profileID string,
) (participantLaunchSpec, error) {
	var spec participantLaunchSpec
	var sessionState, participantState string
	if err := service.database.QueryRowContext(ctx, `
SELECT session.game_id,session.game_variant_revision_id,session.core_artifact_id,session.state,
  participant.player_no,participant.state,participant.credential_generation
FROM netplay_sessions session
JOIN netplay_session_participants participant ON participant.netplay_session_id=session.id
WHERE session.id=? AND session.room_id=? AND participant.profile_id=?
`, sessionID, roomID, profileID).Scan(
		&spec.gameID, &spec.revisionID, &spec.artifactID, &sessionState,
		&spec.playerNo, &participantState, &spec.generation,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return participantLaunchSpec{}, ErrSessionNotFound
		}
		return participantLaunchSpec{}, serviceError("load participant launch", err)
	}
	if sessionState == "FINISHED" || sessionState == "FAILED" ||
		(participantState != "LOCKED" && participantState != "LAUNCH_READY" && participantState != "RUNTIME_READY") {
		return participantLaunchSpec{}, ErrRoomConflict
	}
	if spec.generation == 0 {
		spec.generation = 1
	}
	return spec, nil
}

func (service *Service) participantCredential(
	sessionID, profileID string, generation int64,
) ([32]byte, error) {
	const maxCredentialGeneration = int64(1<<32 - 1)
	if generation < 1 || generation > maxCredentialGeneration {
		return [32]byte{}, ErrForbidden
	}
	sessionUUID, err := uuid.Parse(sessionID)
	if err != nil {
		return [32]byte{}, ErrSessionNotFound
	}
	profileUUID, err := uuid.Parse(profileID)
	if err != nil {
		return [32]byte{}, ErrForbidden
	}
	return service.credentials.Capability(sessionUUID, profileUUID, uint32(generation)), nil
}

func (service *Service) recordParticipantLaunch(
	ctx context.Context, roomID, sessionID, profileID string, playerNo int, now int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("record participant launch transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var transitionRecorded int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_events
WHERE room_id=? AND netplay_session_id=? AND profile_id=? AND event_type='PARTICIPANT_STATE_CHANGED'
`, roomID, sessionID, profileID).Scan(&transitionRecorded); err != nil {
		return serviceError("check participant launch event", err)
	}
	if transitionRecorded == 0 {
		data := map[string]any{
			"schemaVersion": 1, "fromState": "LOCKED", "toState": "LAUNCH_READY",
		}
		if err := appendEvent(
			ctx, transaction, roomID, &sessionID, &profileID, &playerNo,
			"PARTICIPANT_STATE_CHANGED", data, now,
		); err != nil {
			return err
		}
	}
	if err := service.advanceSessionToLoading(ctx, transaction, roomID, sessionID, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("record participant launch commit", err)
	}
	return nil
}

func (service *Service) advanceSessionToLoading(
	ctx context.Context, transaction *sql.Tx, roomID, sessionID string, now int64,
) error {
	var remaining int
	if err := transaction.QueryRowContext(ctx, `
SELECT count(*) FROM netplay_session_participants
WHERE netplay_session_id=? AND state='LOCKED'
`, sessionID).Scan(&remaining); err != nil {
		return serviceError("count locked participants", err)
	}
	if remaining != 0 {
		return nil
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_sessions SET state='LOADING',version=version+1,updated_at_ms=? WHERE id=? AND state='PREPARING'
`, now, sessionID)
	if err != nil {
		return serviceError("advance session to loading", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil
	}
	data := map[string]any{
		"schemaVersion": 1, "fromState": "PREPARING", "toState": "LOADING",
	}
	return appendEvent(
		ctx, transaction, roomID, &sessionID, nil, nil, "SESSION_STATE_CHANGED", data, now,
	)
}

func (service *Service) Events(ctx context.Context, roomID string, afterID int64, limit int) ([]Event, error) {
	var exists int
	if err := service.database.QueryRowContext(
		ctx, `SELECT count(*) FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&exists); err != nil {
		return nil, serviceError("check event room", err)
	}
	if exists != 1 {
		return nil, ErrRoomNotFound
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT id,event_type,data_json,created_at_ms FROM netplay_events
WHERE room_id=? AND id>? ORDER BY id LIMIT ?
	`, roomID, afterID, limit)
	if err != nil {
		return nil, serviceError("list events", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		var data string
		if err := rows.Scan(&event.ID, &event.EventType, &data, &event.CreatedAt); err != nil {
			return nil, serviceError("scan event", err)
		}
		if err := json.Unmarshal([]byte(data), &event.Data); err != nil {
			return nil, fmt.Errorf("netplay/event data: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, serviceError("list events", err)
	}
	return result, nil
}

func (service *Service) ExpireRooms(ctx context.Context) error {
	now := service.clock.Now().UnixMilli()
	candidates, err := service.passiveExpiryCandidates(ctx, now)
	if err != nil {
		return err
	}
	for _, item := range candidates {
		if err := service.expirePassiveRoom(ctx, item, now); err != nil {
			return err
		}
	}
	active, err := service.activeExpiryCandidates(ctx, now)
	if err != nil {
		return err
	}
	for _, item := range active {
		if err := service.endRoomSystem(ctx, item.roomID, item.reason); err != nil && !errors.Is(err, ErrRoomNotFound) {
			return fmt.Errorf("netplay/end timed out session: %w", err)
		}
	}
	return nil
}

type passiveExpiryCandidate struct {
	roomID    string
	profileID string
}

func (service *Service) passiveExpiryCandidates(
	ctx context.Context, now int64,
) ([]passiveExpiryCandidate, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,host_profile_id FROM netplay_rooms
WHERE state IN ('DRAFT','WAITING') AND expires_at_ms<=?
ORDER BY expires_at_ms,id LIMIT 100
`, now)
	if err != nil {
		return nil, fmt.Errorf("netplay/find expired rooms: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	candidates := make([]passiveExpiryCandidate, 0, 100)
	for rows.Next() {
		var item passiveExpiryCandidate
		if err := rows.Scan(&item.roomID, &item.profileID); err != nil {
			return nil, serviceError("scan expired room", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, serviceError("find expired rooms", err)
	}
	return candidates, nil
}

func (service *Service) expirePassiveRoom(
	ctx context.Context, item passiveExpiryCandidate, now int64,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return serviceError("expire room transaction", err)
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE netplay_rooms SET state='EXPIRED',ended_at_ms=?,end_reason='HARD_EXPIRED',version=version+1,updated_at_ms=?
WHERE id=? AND state IN ('DRAFT','WAITING') AND expires_at_ms<=?
`, now, now, item.roomID, now)
	if err != nil {
		return serviceError("expire room", err)
	}
	if affected, _ := result.RowsAffected(); affected == 1 {
		data := map[string]any{"schemaVersion": 1, "reason": "HARD_EXPIRED"}
		if err := appendEvent(
			ctx, transaction, item.roomID, nil, &item.profileID, nil, "ROOM_EXPIRED", data, now,
		); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return serviceError("expire room commit", err)
	}
	return nil
}

type activeExpiryCandidate struct {
	roomID string
	reason string
}

func (service *Service) activeExpiryCandidates(
	ctx context.Context, now int64,
) ([]activeExpiryCandidate, error) {
	activeRows, err := service.database.QueryContext(ctx, `
SELECT room.id,
  CASE WHEN room.state='STARTING' THEN 'START_TIMEOUT' ELSE 'HARD_EXPIRED' END
FROM netplay_rooms room
LEFT JOIN netplay_sessions session ON session.id=room.current_session_id
WHERE (room.state='STARTING' AND room.updated_at_ms<=?)
   OR (room.state='RUNNING' AND session.started_at_ms IS NOT NULL AND session.started_at_ms<=?)
ORDER BY room.updated_at_ms,room.id LIMIT 100
	`, now-(2*time.Minute).Milliseconds(), now-(8*time.Hour).Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("netplay/find timed out sessions: %w", err)
	}
	defer func() { cleanup.Error("close", activeRows.Close()) }()
	active := make([]activeExpiryCandidate, 0, 100)
	for activeRows.Next() {
		var item activeExpiryCandidate
		if err := activeRows.Scan(&item.roomID, &item.reason); err != nil {
			return nil, serviceError("scan timed out session", err)
		}
		active = append(active, item)
	}
	if err := activeRows.Err(); err != nil {
		return nil, serviceError("find timed out sessions", err)
	}
	return active, nil
}

func (service *Service) mutateHostRoom(
	ctx context.Context,
	roomID, actorProfileID string,
	expectedVersion int64,
	allowedStates []string,
	mutation func(*sql.Tx, int64) error,
) (Room, error) {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, serviceError("mutate host room transaction", err)
	}
	defer cleanup.Rollback(transaction)
	var host, state string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT host_profile_id,state,version FROM netplay_rooms WHERE id=?`, roomID,
	).Scan(&host, &state, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Room{}, ErrRoomNotFound
		}
		return Room{}, serviceError("mutate host room state", err)
	}
	if host != actorProfileID {
		return Room{}, ErrForbidden
	}
	if version != expectedVersion {
		return Room{}, ErrPrecondition
	}
	if !slices.Contains(allowedStates, state) {
		return Room{}, ErrRoomConflict
	}
	if err := mutation(transaction, service.clock.Now().UnixMilli()); err != nil {
		return Room{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Room{}, serviceError("mutate host room commit", err)
	}
	return service.Room(ctx, roomID, actorProfileID)
}

func appendEvent(
	ctx context.Context,
	transaction *sql.Tx,
	roomID string,
	sessionID, profileID *string,
	playerNo *int,
	eventType string,
	data map[string]any,
	now int64,
) error {
	if data == nil {
		data = map[string]any{"schemaVersion": 1}
	}
	encoded, err := json.Marshal(data)
	if err != nil || len(encoded) > 4096 {
		return errEventData
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO netplay_events(room_id,netplay_session_id,profile_id,player_no,event_type,data_json,created_at_ms)
VALUES(?,?,?,?,?,?,?)
`, roomID, sessionID, profileID, playerNo, eventType, string(encoded), now); err != nil {
		return fmt.Errorf("netplay/append event: %w", err)
	}
	return nil
}

func newV7() string {
	value, err := uuid.NewV7()
	if err != nil {
		return ""
	}
	return value.String()
}

func intPointer(value int) *int { return &value }

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func stringPointerFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
