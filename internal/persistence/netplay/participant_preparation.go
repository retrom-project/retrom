package netplay

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/service/netplay"
)

type ParticipantPreparation struct{ database *sql.DB }

func NewParticipantPreparation(database *sql.DB) *ParticipantPreparation {
	return &ParticipantPreparation{database}
}

func (repository *ParticipantPreparation) Snapshot(
	ctx context.Context,
	roomID, sessionID, profileID string,
) (netplay.PreparationSnapshot, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return netplay.PreparationSnapshot{}, fmt.Errorf("netplay/begin preparation read: %w", err)
	}
	defer dbexec.Rollback(tx)
	before, err := (preparationRecords{tx}).Snapshot(ctx, roomID, sessionID, profileID)
	if err != nil {
		return netplay.PreparationSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return netplay.PreparationSnapshot{}, fmt.Errorf("netplay/commit preparation read: %w", err)
	}
	return before, nil
}

func (repository *ParticipantPreparation) WithPreparation(
	ctx context.Context,
	work func(netplay.PreparationScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("netplay/begin preparation record: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := preparationRecords{tx}
	if err := work(netplay.PreparationScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("netplay/commit preparation record: %w", err)
	}
	return nil
}

type preparationRecords struct{ executor dbexec.Executor }

func (records preparationRecords) Snapshot(
	ctx context.Context,
	roomID, sessionID, profileID string,
) (netplay.PreparationSnapshot, error) {
	control, err := sessionControlRecords(records).Current(ctx, roomID, sessionID)
	if err != nil {
		return netplay.PreparationSnapshot{}, err
	}
	before := netplay.PreparationSnapshot{Control: control}
	found := false
	for _, peer := range control.Peers {
		if peer.ProfileID == profileID {
			before.Peer = peer
			found = true
		}
		if peer.State == "LOCKED" {
			before.Locked++
		}
	}
	if !found {
		return netplay.PreparationSnapshot{}, netplay.ErrSessionNotFound
	}
	err = records.executor.QueryRowContext(ctx, `
SELECT game_id,game_variant_id,provider_id,target_id,bundle_sha256,
EXISTS(SELECT 1 FROM netplay_events WHERE room_id=? AND netplay_session_id=?
AND profile_id=? AND event_type='PARTICIPANT_STATE_CHANGED')
FROM netplay_sessions WHERE id=? AND room_id=?`, roomID, sessionID, profileID, sessionID, roomID).Scan(
		&before.GameID,
		&before.VariantID,
		&before.ProviderID,
		&before.TargetID,
		&before.BundleSHA256,
		&before.LaunchRecorded,
	)
	if err != nil {
		return netplay.PreparationSnapshot{}, fmt.Errorf("netplay/read preparation metadata: %w", err)
	}
	return before, nil
}

func (records preparationRecords) Record(ctx context.Context, plan netplay.PreparationPlan) error {
	control := sessionControlRecords(records)
	if err := control.fence(ctx, plan.Before.Control); err != nil {
		return err
	}
	if err := records.fencePeer(ctx, plan.Before); err != nil {
		return err
	}
	if plan.AdvanceLoading {
		return control.Session(
			ctx,
			netplay.SessionTransitionPlan{Before: plan.Before.Control, Target: "LOADING", Events: plan.Events, Now: plan.Now},
		)
	}
	result, err := recordstore.UpdateNetplaySessions(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `version=version`,
			Scope: recordstore.Scope{
				Where: `id=? AND room_id=? AND version=? AND state=?`,
				Args: []any{
					plan.Before.Control.SessionID,
					plan.Before.Control.RoomID,
					plan.Before.Control.Version,
					plan.Before.Control.State,
				},
			},
		},
	)
	if err := requireRoomChange(result, err, netplay.ErrRoomConflict); err != nil {
		return err
	}
	return control.events(ctx, plan.Before.Control, plan.Events, plan.Now)
}

func (records preparationRecords) fencePeer(ctx context.Context, before netplay.PreparationSnapshot) error {
	result, err := recordstore.UpdateNetplaySessionParticipants(
		ctx,
		records.executor,
		recordstore.Update{
			Set: `version=version`,
			Scope: recordstore.Scope{
				Where: `netplay_session_id=? AND profile_id=? AND player_no=?
AND credential_generation=? AND version=? AND state=?`,
				Args: []any{
					before.Control.SessionID,
					before.Peer.ProfileID,
					before.Peer.PlayerNo,
					before.Peer.CredentialGeneration,
					before.Peer.Version,
					before.Peer.State,
				},
			},
		},
	)
	return requireRoomChange(result, err, netplay.ErrRoomConflict)
}
