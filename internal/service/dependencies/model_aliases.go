package dependencies

import model "retrom/internal/model/dependencies"

type (
	ActivationState    = model.ActivationState
	BIOSRecords        = model.BIOSRecords
	BIOSRequirement    = model.BIOSRequirement
	CatalogPublication = model.CatalogPublication
	CatalogRecords     = model.CatalogRecords
	CatalogStats       = model.CatalogStats
	DATLookup          = model.DATLookup
	DATRecords         = model.DATRecords
	DATRegistration    = model.DATRegistration
	DATSelection       = model.DATSelection
	DATState           = model.DATState
	Job                = model.Job
	JobClaim           = model.JobClaim
	JobCreation        = model.JobCreation
	JobFinish          = model.JobFinish
	JobRecords         = model.JobRecords
	Repository         = model.Repository
	RegisteredDAT      = model.RegisteredDAT
	RuntimeTarget      = model.RuntimeTarget
	TargetRecords      = model.TargetRecords
	WriteScope         = model.WriteScope
)

var ErrDATJobNotClaimed = model.ErrDATJobNotClaimed
