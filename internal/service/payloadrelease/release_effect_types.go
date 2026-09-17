package payloadrelease

import (
	model "retrom/internal/model/payloadrelease"
	"time"
)

type ReleaseEffects struct {
	repository model.EffectRepository
	authority  model.EffectAuthority
	gc         model.GCStager
	waiter     model.EffectWaiter
	now        func() time.Time
}

func NewReleaseEffects(
	repository model.EffectRepository,
	authority model.EffectAuthority,
	gc model.GCStager,
	waiter model.EffectWaiter,
	now func() time.Time,
) *ReleaseEffects {
	return &ReleaseEffects{repository: repository, authority: authority, gc: gc, waiter: waiter, now: now}
}
