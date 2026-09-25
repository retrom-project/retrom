package launch

import (
	"context"
	"fmt"
	"math"
)

func validPlayEvent(kind string, event PlayEvent) bool {
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
func validPlayProgress(source PlaySource, current PlayRecord, event PlayEvent) bool {
	return source.Session.State == "ACTIVE" && current.State == "ACTIVE" &&
		event.PreviousInterval != nil &&
		current.LastSequence < math.MaxInt64 && event.ClientSequence == current.LastSequence+1
}

func acceptedPlayDuration(interval Interval, lastHeartbeat, now int64) int64 {
	if !interval.Running || !interval.Visible || interval.Paused || now <= lastHeartbeat {
		return 0
	}
	// Compare before subtracting to avoid overflowing on corrupted persisted times.
	if lastHeartbeat < 0 && now > math.MaxInt64+lastHeartbeat {
		return 45_000
	}
	return min(now-lastHeartbeat, 45_000)
}

func replayPlayEvent(ctx context.Context, reader PlayReader, playID, kind string, event PlayEvent) (PlayResult, error) {
	stored, found, err := reader.Event(ctx, playID, event.ClientSequence)
	if err != nil {
		return PlayResult{}, fmt.Errorf("read previous play event: %w", err)
	}
	expected := map[string]string{"start": "START", "heartbeat": "HEARTBEAT", "finish": "FINISH"}[kind]
	intervalMatches := event.PreviousInterval == nil && stored.Kind == "START" ||
		event.PreviousInterval != nil && *event.PreviousInterval == stored.Interval
	if !found || stored.Kind != expected || stored.ClientObservedAtMS != event.ClientObservedAtMS || !intervalMatches {
		return PlayResult{}, ErrBlocked
	}
	state := "ACTIVE"
	if stored.Kind == "FINISH" {
		state = "FINISHED"
	}
	return PlayResult{
		PlaySessionID:    playID,
		ClientSequence:   event.ClientSequence,
		AcceptedDuration: stored.AcceptedDurationMS,
		State:            state,
	}, nil
}

func finishUnstartedPlay(ctx context.Context, writer PlayWriter, source PlaySource, now int64) (PlayResult, error) {
	if source.Session.State == "FINISHED" {
		return PlayResult{State: "FINISHED"}, nil
	}
	if !playVersionWritable(source.Version) {
		return PlayResult{}, ErrBlocked
	}
	if err := writer.Finish(ctx, PlayFinish{Source: source, NowMS: now}); err != nil {
		return PlayResult{}, fmt.Errorf("finish unstarted launch: %w", err)
	}
	return PlayResult{State: "FINISHED"}, nil
}

func recordPreviewPlay(
	ctx context.Context,
	writer PlayWriter,
	source PlaySource,
	kind string,
	event PlayEvent,
	now int64,
) (PlayResult, error) {
	state := source.Session.State
	if state == "FINISHED" && kind == "finish" {
		return PlayResult{ClientSequence: event.ClientSequence, State: state}, nil
	}
	if state != "ACTIVE" && (state != "CREATED" || kind != "finish") {
		return PlayResult{}, ErrBlocked
	}
	if kind == "finish" {
		if !playVersionWritable(source.Version) {
			return PlayResult{}, ErrBlocked
		}
		if err := writer.Finish(ctx, PlayFinish{Source: source, NowMS: now}); err != nil {
			return PlayResult{}, fmt.Errorf("finish review trial: %w", err)
		}
		state = "FINISHED"
	}
	return PlayResult{ClientSequence: event.ClientSequence, State: state}, nil
}
