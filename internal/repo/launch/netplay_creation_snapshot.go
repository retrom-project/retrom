package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
)

func (records netplayCreationRecords) Snapshot(
	ctx context.Context,
	request application.NetplayCreateRequest,
) (application.NetplayCreationSnapshot, error) {
	authority, existingID, found, err := netplayCreationAuthority(ctx, records.executor, request)
	if err != nil || !found {
		return application.NetplayCreationSnapshot{}, err
	}
	snapshot := application.NetplayCreationSnapshot{Found: true, Authority: authority}
	source, found, err := productCreationSource(ctx, records.executor, application.ProductCreateCommand{
		ProfileID: request.ProfileID, Request: application.CreateRequest{GameID: authority.GameID, CoreID: &authority.CoreID},
	}, nil)
	if err != nil || !found {
		return snapshot, err
	}
	snapshot.Product, err = ProductContentSnapshot(ctx, records.executor, source)
	if err != nil {
		return application.NetplayCreationSnapshot{}, err
	}
	if existingID != nil {
		snapshot.Existing, err = netplayExistingLaunch(ctx, records.executor, *existingID)
		if err != nil {
			return application.NetplayCreationSnapshot{}, err
		}
	}
	return snapshot, nil
}

func netplayCreationAuthority(
	ctx context.Context,
	executor dbexec.Executor,
	request application.NetplayCreateRequest,
) (application.NetplayCreationAuthority, *string, bool, error) {
	var authority application.NetplayCreationAuthority
	var existing *string
	err := executor.QueryRowContext(ctx, `SELECT session.id,session.room_id,session.game_id,session.game_variant_id,
 variant.core_id,session.provider_id,session.target_id,session.bundle_sha256,session.state,
 room.state,COALESCE(room.current_session_id,''),participant.profile_id,participant.room_member_id,participant.state,
 participant.player_no,participant.credential_generation,participant.version,
 participant.credential_sha256,participant.launch_session_id
FROM netplay_sessions session
JOIN netplay_rooms room ON room.id=session.room_id
JOIN game_variants variant ON variant.id=session.game_variant_id AND variant.game_id=session.game_id
JOIN netplay_session_participants participant ON participant.netplay_session_id=session.id
JOIN netplay_room_members member ON member.id=participant.room_member_id AND member.room_id=room.id
 AND member.profile_id=participant.profile_id AND member.player_no=participant.player_no AND member.left_at_ms IS NULL
WHERE session.id=? AND participant.profile_id=? AND participant.player_no=?`,
		request.SessionID, request.ProfileID, request.PlayerNo).Scan(

		&authority.SessionID,
		&authority.RoomID,
		&authority.GameID,
		&authority.VariantID,
		&authority.CoreID,

		&authority.ProviderID,
		&authority.TargetID,
		&authority.BundleDigest,
		&authority.SessionState,
		&authority.RoomState,

		&authority.CurrentSessionID,
		&authority.ProfileID,
		&authority.MemberID,
		&authority.ParticipantState,

		&authority.PlayerNo,
		&authority.Generation,
		&authority.ParticipantVersion,
		&authority.CredentialHash,
		&existing,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.NetplayCreationAuthority{}, nil, false, nil
	}
	if err != nil {
		return application.NetplayCreationAuthority{}, nil, false, fmt.Errorf("read netplay participant authority: %w", err)
	}
	return authority, existing, true, nil
}

func netplayExistingLaunch(
	ctx context.Context,
	executor dbexec.Executor,
	id string,
) (*application.NetplayExistingLaunch, error) {
	var existing application.NetplayExistingLaunch
	err := executor.QueryRowContext(ctx, `SELECT id,profile_id,game_id,provider_id,target_id,bundle_sha256,state,
 netplay_session_id,netplay_player_no,credential_sha256,bootstrap_expires_at_ms,hard_expires_at_ms
FROM launch_sessions WHERE id=?`, id).Scan(&existing.ID, &existing.ProfileID, &existing.GameID, &existing.ProviderID,
		&existing.TargetID, &existing.BundleDigest, &existing.State, &existing.SessionID,
		&existing.PlayerNo, &existing.CredentialHash,
		&existing.BootstrapEnd, &existing.HardEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, application.ErrBlocked
	}
	if err != nil {
		return nil, fmt.Errorf("read existing netplay launch: %w", err)
	}
	return &existing, nil
}
