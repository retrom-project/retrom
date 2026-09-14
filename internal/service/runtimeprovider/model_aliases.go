package runtimeprovider

import model "retrom/internal/model/runtimeprovider"

var NewProjection = model.NewProjection

type (
	Audit              = model.Audit
	CatalogRecords     = model.CatalogRecords
	CurrentProvider    = model.CurrentProvider
	CurrentState       = model.CurrentState
	Projection         = model.Projection
	ProjectionRecords  = model.ProjectionRecords
	ProviderProjection = model.ProviderProjection
	Publication        = model.Publication
	Repository         = model.Repository
	TargetIdentity     = model.TargetIdentity
	TargetProjection   = model.TargetProjection
	WriteScope         = model.WriteScope
)

var (
	ErrProjectionInvalid            = model.ErrProjectionInvalid
	ErrProviderCheckpointUnreadable = model.ErrProviderCheckpointUnreadable
	ErrProviderDowngrade            = model.ErrProviderDowngrade
	ErrProviderTargetReferenced     = model.ErrProviderTargetReferenced
	ErrProviderVersionRebuilt       = model.ErrProviderVersionRebuilt
)
