package payloadrelease

import (
	"time"
)

type ReleaseEffects struct {
	repository EffectRepository
	authority  EffectAuthority
	gc         GCStager
	waiter     EffectWaiter
	now        func() time.Time
}

func NewReleaseEffects(
	repository EffectRepository,
	authority EffectAuthority,
	gc GCStager,
	waiter EffectWaiter,
	now func() time.Time,
) *ReleaseEffects {
	return &ReleaseEffects{repository: repository, authority: authority, gc: gc, waiter: waiter, now: now}
}
