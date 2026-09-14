package storageanalysis

import model "retrom/internal/model/storageanalysis"

type (
	ArchiveMember  = model.ArchiveMember
	ReadModel      = model.ReadModel
	Repository     = model.Repository
	SaveReferences = model.SaveReferences
	Usage          = model.Usage
)

const (
	UsageBIOS     = model.UsageBIOS
	UsageGame     = model.UsageGame
	UsageMedia    = model.UsageMedia
	UsageRuntime  = model.UsageRuntime
	UsageSaves    = model.UsageSaves
	UsageWorkflow = model.UsageWorkflow
)
