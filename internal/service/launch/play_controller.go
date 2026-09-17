package launch

import (
	"context"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/launch"

	"github.com/google/uuid"
)

const playIdleLeaseMS int64 = 120_000

type PlayController struct {
	repository model.PlayRepository
	policy     accessPolicy
	newID      func() (string, error)
}

func NewPlayController(repository model.PlayRepository, now func() time.Time, matches model.MatchCapability) *PlayController {
	return &PlayController{repository: repository, policy: accessPolicy{now: now, matches: matches}, newID: newPlayID}
}

func newPlayID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create play session identity: %w", err)
	}
	return id.String(), nil
}

func (service *PlayController) RecordPlay(
	ctx context.Context,
	id, capability, kind string,
	event model.PlayEvent,
) (model.PlayResult, error) {
	if !validPlayEvent(kind, event) {
		return model.PlayResult{}, model.ErrBlocked
	}
	var result model.PlayResult
	err := service.repository.WithPlay(ctx, func(scope model.PlayScope) error {
		source, found, err := scope.Read.Source(ctx, id)
		if err != nil {
			return fmt.Errorf("read play authority: %w", err)
		}
		now := service.policy.now().UnixMilli()
		if !found || !service.authorized(source, capability, now) {
			return model.ErrCredential
		}
		if source.Ref.Preview {
			result, err = recordPreviewPlay(ctx, scope.Write, source, kind, event, now)
		} else {
			result, err = service.recordProductPlay(ctx, scope, source, kind, event, now)
		}
		return err
	})
	if err != nil {
		return model.PlayResult{}, fmt.Errorf("record play event: %w", err)
	}
	return result, nil
}

func (service *PlayController) authorized(source model.PlaySource, capability string, now int64) bool {
	state := source.Session.State
	return (state == "CREATED" || state == "ACTIVE" || state == "FINISHED") && source.Session.HardExpiresAtMS > now &&
		service.policy.matches != nil && service.policy.matches(capability, source.Session.CredentialHash)
}

func (service *PlayController) recordProductPlay(
	ctx context.Context,
	scope model.PlayScope,
	source model.PlaySource,
	kind string,
	event model.PlayEvent,
	now int64,
) (model.PlayResult, error) {
	if source.ProfileID == "" || source.GameID == "" {
		return model.PlayResult{}, model.ErrBlocked
	}
	current, found, err := scope.Read.Current(ctx, source.Ref.ID)
	if err != nil {
		return model.PlayResult{}, fmt.Errorf("read current play session: %w", err)
	}
	if found && event.ClientSequence <= current.LastSequence {
		return replayPlayEvent(ctx, scope.Read, current.ID, kind, event)
	}
	if !found && kind == "finish" && event.ClientSequence == 0 {
		return finishUnstartedPlay(ctx, scope.Write, source, now)
	}
	if kind == "start" {
		return service.startPlay(ctx, scope.Write, source, event, now)
	}
	if !found || !validPlayProgress(source, current, event, now) {
		return model.PlayResult{}, model.ErrBlocked
	}
	return recordPlayProgress(ctx, scope.Write, source, current, kind, event, now)
}

func recordPlayProgress(ctx context.Context, writer model.PlayWriter, source model.PlaySource, current model.PlayRecord,
	kind string, event model.PlayEvent, now int64,
) (model.PlayResult, error) {
	accepted := acceptedPlayDuration(*event.PreviousInterval, current.LastHeartbeatAtMS, now)
	if !playVersionWritable(source.Version) || !playVersionWritable(current.Version) ||
		current.ActiveDurationMS < 0 || current.ActiveDurationMS > math.MaxInt64-accepted ||
		now > math.MaxInt64-playIdleLeaseMS {
		return model.PlayResult{}, model.ErrBlocked
	}
	plan := model.PlayProgress{
		Source: source, Current: current, Event: event, Kind: kind,
		AcceptedDurationMS: accepted, NowMS: now, IdleExpiresAtMS: now + playIdleLeaseMS,
	}
	if err := writer.Progress(ctx, plan); err != nil {
		return model.PlayResult{}, fmt.Errorf("persist play progress: %w", err)
	}
	state := "ACTIVE"
	if kind == "finish" {
		state = "FINISHED"
	}
	return model.PlayResult{
		PlaySessionID:    current.ID,
		ClientSequence:   event.ClientSequence,
		AcceptedDuration: accepted,
		State:            state,
	}, nil
}

func (service *PlayController) startPlay(
	ctx context.Context,
	writer model.PlayWriter,
	source model.PlaySource,
	event model.PlayEvent,
	now int64,
) (model.PlayResult, error) {
	if source.Session.State != "ACTIVE" || !playVersionWritable(source.Version) || now > math.MaxInt64-playIdleLeaseMS {
		return model.PlayResult{}, model.ErrBlocked
	}
	id, err := service.newID()
	if err != nil {
		return model.PlayResult{}, fmt.Errorf("prepare play identity: %w", err)
	}
	if err := writer.Start(
		ctx,
		model.PlayStart{Source: source, PlayID: id, Event: event, NowMS: now, IdleExpiresAtMS: now + playIdleLeaseMS},
	); err != nil {
		return model.PlayResult{}, fmt.Errorf("persist play start: %w", err)
	}
	return model.PlayResult{PlaySessionID: id, ClientSequence: 0, State: "ACTIVE"}, nil
}
