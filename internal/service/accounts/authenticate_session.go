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
	if !found || !model.ValidSession(snapshot, now) {
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
	snapshot, err := service.repository.CommitRefreshSession(ctx, model.RefreshSessionCommand{
		Digest: digest,
		NowMS:  service.now().UnixMilli(),
	})
	if err != nil {
		return model.SessionSnapshot{}, fmt.Errorf("commit session refresh: %w", err)
	}
	return snapshot, nil
}
