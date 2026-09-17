package launch

import (
	"context"
	"fmt"
	"math"
	model "retrom/internal/model/launch"
)

func validPlayEvent(kind string, event model.PlayEvent) bool {
	if event.ClientObservedAtMS < 0 || event.ClientObservedAtMS > 253402300799999 || event.ClientSequence < 0 {
		return false
	}
	if kind == "start" {
		return event.ClientSequence == 0 && event.PreviousInterval == nil
	}
	if kind != "heartbeat" && kind != "finish" {
		return false
	}
	return event.ClientSequence > 0 && event.PreviousInterval != nil ||
		kind == "finish" && event.ClientSequence == 0 && event.PreviousInterval == nil
}
func playVersionWritable(version int64) bool { return version >= 1 && version < math.MaxInt64 }
func validPlayProgress(source model.PlaySource, current model.PlayRecord, event model.PlayEvent, now int64) bool {
	return source.Session.State == "ACTIVE" && current.State == "ACTIVE" &&
		(source.IdleExpiresAtMS == nil || *source.IdleExpiresAtMS > now) && event.PreviousInterval != nil &&
		current.LastSequence < math.MaxInt64 && event.ClientSequence == current.LastSequence+1
}

func acceptedPlayDuration(interval model.Interval, lastHeartbeat, now int64) int64 {
	if !interval.Running || !interval.Visible || interval.Paused || now <= lastHeartbeat {
		return 0
	}
	// Compare before subtracting to avoid overflowing on corrupted persisted times.
	if lastHeartbeat < 0 && now > math.MaxInt64+lastHeartbeat {
		return 45_000
	}
	return min(now-lastHeartbeat, 45_000)
}

func replayPlayEvent(ctx context.Context, reader model.PlayReader, playID, kind string, event model.PlayEvent) (model.PlayResult, error) {
	stored, found, err := reader.Event(ctx, playID, event.ClientSequence)
	if err != nil {
		return model.PlayResult{}, fmt.Errorf("read previous play event: %w", err)
	}
	expected := map[string]string{"start": "START", "heartbeat": "HEARTBEAT", "finish": "FINISH"}[kind]
	intervalMatches := event.PreviousInterval == nil && stored.Kind == "START" ||
		event.PreviousInterval != nil && *event.PreviousInterval == stored.Interval
	if !found || stored.Kind != expected || stored.ClientObservedAtMS != event.ClientObservedAtMS || !intervalMatches {
		return model.PlayResult{}, model.ErrBlocked
	}
	state := "ACTIVE"
	if stored.Kind == "FINISH" {
		state = "FINISHED"
	}
	return model.PlayResult{
		PlaySessionID:    playID,
		ClientSequence:   event.ClientSequence,
		AcceptedDuration: stored.AcceptedDurationMS,
		State:            state,
	}, nil
}

func finishUnstartedPlay(ctx context.Context, writer model.PlayWriter, source model.PlaySource, now int64) (model.PlayResult, error) {
	if source.Session.State == "FINISHED" {
		return model.PlayResult{State: "FINISHED"}, nil
	}
	if !playVersionWritable(source.Version) {
		return model.PlayResult{}, model.ErrBlocked
	}
	if err := writer.Finish(ctx, model.PlayFinish{Source: source, NowMS: now}); err != nil {
		return model.PlayResult{}, fmt.Errorf("finish unstarted launch: %w", err)
	}
	return model.PlayResult{State: "FINISHED"}, nil
}

func recordPreviewPlay(
	ctx context.Context,
	writer model.PlayWriter,
	source model.PlaySource,
	kind string,
	event model.PlayEvent,
	now int64,
) (model.PlayResult, error) {
	state := source.Session.State
	if state == "FINISHED" && kind == "finish" {
		return model.PlayResult{ClientSequence: event.ClientSequence, State: state}, nil
	}
	if state != "ACTIVE" && (state != "CREATED" || kind != "finish") {
		return model.PlayResult{}, model.ErrBlocked
	}
	if kind == "finish" {
		if !playVersionWritable(source.Version) {
			return model.PlayResult{}, model.ErrBlocked
		}
		if err := writer.Finish(ctx, model.PlayFinish{Source: source, NowMS: now}); err != nil {
			return model.PlayResult{}, fmt.Errorf("finish review trial: %w", err)
		}
		state = "FINISHED"
	}
	return model.PlayResult{ClientSequence: event.ClientSequence, State: state}, nil
}
