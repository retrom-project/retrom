package diagnostics

import "context"

// Repository provides one consistent read snapshot for diagnostics.
type Repository interface {
	LoadReport(context.Context) (Report, error)
}

type Counts struct {
	PublishedGames      int64
	DeletedGames        int64
	ActiveSaves         int64
	DeletedSaves        int64
	Blobs               int64
	QueuedJobs          int64
	RunningJobs         int64
	CancelRequestedJobs int64
	SucceededJobs       int64
	FailedJobs          int64
	CancelledJobs       int64
	PendingDATs         int64
	ParsingDATs         int64
	ReadyDATs           int64
	FailedDATs          int64
	CancelledDATs       int64
}

type RuntimeProvider struct {
	ProviderID      string
	ProviderVersion string
	BundleSHA256    string
	Source          string
}

type Report struct {
	DatabaseSchemaVersion int64
	RuntimeProviders      []RuntimeProvider
	Counts                Counts
}
