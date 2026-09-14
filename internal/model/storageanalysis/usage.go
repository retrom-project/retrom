package storageanalysis

type Usage uint8

const (
	UsageGame Usage = 1 << iota
	UsageBIOS
	UsageSaves
	UsageMedia
	UsageWorkflow
	UsageRuntime
)
