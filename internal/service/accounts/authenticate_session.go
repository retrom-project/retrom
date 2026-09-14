package accounts

import (
	"context"
	"crypto/sha256"
	"fmt"
)

func (service *Authentication) Authenticate(ctx context.Context, token string) (Session, error) {
	raw, err := DecodeToken(token)
	if err != nil {
		return Session{}, ErrAuthenticationNeeded
	}
	digest := sha256.Sum256(raw)
	snapshot, found, err := service.repository.Session(ctx, digest)
	if err != nil {
		return Session{}, fmt.Errorf("read authentication session: %w", err)
	}
	now := service.now().UnixMilli()
	if !found || !validSession(snapshot, now) {
		return Session{}, ErrAuthenticationNeeded
	}
	if now-snapshot.LastSeen >= refreshInterval.Milliseconds() {
		snapshot, err = service.refresh(ctx, digest)
		if err != nil {
			return Session{}, err
		}
	}
	return snapshot.View(token, raw), nil
}

func (service *Authentication) refresh(ctx context.Context, digest [32]byte) (SessionSnapshot, error) {
	var snapshot SessionSnapshot
	err := service.repository.WithWrite(ctx, func(scope AuthScope) error {
		current, found, err := scope.Read.Session(ctx, digest)
		if err != nil {
			return fmt.Errorf("recheck authentication session: %w", err)
		}
		now := service.now().UnixMilli()
		if !found || !validSession(current, now) {
			return ErrAuthenticationNeeded
		}
		if now-current.LastSeen >= refreshInterval.Milliseconds() {
			expiry := min(now+idleDuration.Milliseconds(), current.AbsoluteExpiry)
			if err := scope.Write.Refresh(
				ctx,
				SessionRefresh{
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
		return SessionSnapshot{}, fmt.Errorf("commit session refresh: %w", err)
	}
	return snapshot, nil
}

func validSession(snapshot SessionSnapshot, now int64) bool {
	return snapshot.RevokedAt == nil && snapshot.Status == "ENABLED" &&
		snapshot.UserVersion == snapshot.SessionVersion &&
		now < snapshot.IdleExpiry && now < snapshot.AbsoluteExpiry
}
