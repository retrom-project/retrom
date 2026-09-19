package accounts

import (
	"context"
	"crypto/sha256"
	"fmt"

	model "retrom/internal/model/accounts"
)

func (service *Authentication) Authenticate(ctx context.Context, token string) (model.Session, error) {
	raw, err := model.DecodeToken(token)
	if err != nil {
		return model.Session{}, model.ErrAuthenticationNeeded
	}
	digest := sha256.Sum256(raw)
	snapshot, found, err := service.repository.Session(ctx, digest)
	if err != nil {
		return model.Session{}, fmt.Errorf("read authentication session: %w", err)
	}
	now := service.now().UnixMilli()
	if !found || !validSession(snapshot, now) {
		return model.Session{}, model.ErrAuthenticationNeeded
	}
	if now-snapshot.LastSeen >= model.RefreshInterval.Milliseconds() {
		snapshot, err = service.refresh(ctx, digest)
		if err != nil {
			return model.Session{}, err
		}
	}
	return snapshot.View(token, raw), nil
}

func (service *Authentication) refresh(ctx context.Context, digest [32]byte) (model.SessionSnapshot, error) {
	var snapshot model.SessionSnapshot
	err := service.repository.WithWrite(ctx, func(scope model.AuthScope) error {
		current, found, err := scope.Read.Session(ctx, digest)
		if err != nil {
			return fmt.Errorf("recheck authentication session: %w", err)
		}
		now := service.now().UnixMilli()
		if !found || !validSession(current, now) {
			return model.ErrAuthenticationNeeded
		}
		if now-current.LastSeen >= model.RefreshInterval.Milliseconds() {
			expiry := min(now+model.IdleDuration.Milliseconds(), current.AbsoluteExpiry)
			if err := scope.Write.Refresh(
				ctx, model.SessionRefresh{
					ID:               current.ID,
					ExpectedLastSeen: current.LastSeen,
					LastSeen:         now,
					IdleExpiry:       expiry,
				},
			); err != nil {
				return fmt.Errorf("refresh authentication session: %w", err)
			}
			current.LastSeen = now
			current.IdleExpiry = expiry
		}
		snapshot = current
		return nil
	})
	if err != nil {
		return model.SessionSnapshot{}, fmt.Errorf("commit session refresh: %w", err)
	}
	return snapshot, nil
}

func validSession(snapshot model.SessionSnapshot, now int64) bool {
	return snapshot.RevokedAt == nil && snapshot.Status == "ENABLED" &&
		snapshot.UserVersion == snapshot.SessionVersion &&
		now < snapshot.IdleExpiry && now < snapshot.AbsoluteExpiry
}
