package launch

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

const maxPlaySnapshotDurationMS int64 = 30 * 24 * 60 * 60 * 1000

type PlayController struct {
	repository PlayRepository
	policy     accessPolicy
	newID      func() (string, error)
}

func NewPlayController(repository PlayRepository, now func() time.Time, matches MatchCapability) *PlayController {
	return &PlayController{repository: repository, policy: accessPolicy{now: now, matches: matches}, newID: newPlayID}
}

func newPlayID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("create play session identity: %w", err)
	}
	return id.String(), nil
}

// RecordSnapshot records a cumulative best-effort observation. It does not
// renew or finish the launch, so missing telemetry cannot revoke game access.
func (service *PlayController) RecordSnapshot(
	ctx context.Context, id, capability string, sample PlaySnapshot,
) (PlaySnapshotResult, error) {
	if sample.ActiveDurationMS < 0 || sample.ActiveDurationMS > maxPlaySnapshotDurationMS {
		return PlaySnapshotResult{}, ErrBlocked
	}
	var result PlaySnapshotResult
	err := service.repository.WithPlay(ctx, func(scope PlayScope) error {
		source, found, err := scope.Read.Source(ctx, id)
		if err != nil {
			return fmt.Errorf("read snapshot authority: %w", err)
		}
		now := service.policy.now().UnixMilli()
		if !found || !service.snapshotAuthorized(source, capability, now) {
			return ErrCredential
		}
		current, hasCurrent, err := scope.Read.Current(ctx, id)
		if err != nil {
			return fmt.Errorf("read snapshot play: %w", err)
		}
		plan := PlaySnapshotPlan{Source: source, NowMS: now, ActiveDurationMS: sample.ActiveDurationMS}
		if hasCurrent {
			if current.State != "ACTIVE" || !playVersionWritable(current.Version) {
				return ErrBlocked
			}
			plan.Current = &current
			plan.PlayID = current.ID
			plan.ActiveDurationMS = max(current.ActiveDurationMS, sample.ActiveDurationMS)
		} else {
			plan.PlayID, err = service.newID()
			if err != nil {
				return fmt.Errorf("create snapshot play identity: %w", err)
			}
		}
		if err := scope.Write.Snapshot(ctx, plan); err != nil {
			return fmt.Errorf("persist play snapshot: %w", err)
		}
		result = PlaySnapshotResult{PlaySessionID: plan.PlayID, ActiveDurationMS: plan.ActiveDurationMS}
		return nil
	})
	if err != nil {
		return PlaySnapshotResult{}, fmt.Errorf("record play snapshot: %w", err)
	}
	return result, nil
}

func (service *PlayController) snapshotAuthorized(source PlaySource, capability string, now int64) bool {
	return source.LaunchID != "" && source.Session.State == "ACTIVE" &&
		source.Session.HardExpiresAtMS > now && service.policy.matches != nil &&
		service.policy.matches(capability, source.Session.CredentialHash) &&
		source.ProfileID != "" && source.GameID != ""
}

func playVersionWritable(version int64) bool { return version >= 1 && version < math.MaxInt64 }
