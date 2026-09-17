package runtimeprovider

import model "retrom/internal/model/runtimeprovider"

var NewProjection = model.NewProjection

type (
	Projection         = model.Projection
	ProviderProjection = model.ProviderProjection
	ReconcileCommand   = model.ReconcileCommand
	Repository         = model.Repository
	TargetProjection   = model.TargetProjection
)

var (
	ErrProjectionInvalid            = model.ErrProjectionInvalid
	ErrProviderCheckpointUnreadable = model.ErrProviderCheckpointUnreadable
	ErrProviderDowngrade            = model.ErrProviderDowngrade
	ErrProviderTargetReferenced     = model.ErrProviderTargetReferenced
	ErrProviderVersionRebuilt       = model.ErrProviderVersionRebuilt
)
