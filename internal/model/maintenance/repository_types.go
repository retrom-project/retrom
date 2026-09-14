package maintenance

import "context"

type Lineage struct {
	Version int64
	Digest  string
}

type Blob struct {
	SHA256    string
	SizeBytes int64
}

type UploadPart struct {
	StorageKey, SHA256 string
	SizeBytes          int64
}

type Snapshot struct {
	Lineage Lineage
	Blobs   []Blob
	Parts   []UploadPart
}

type Repository interface {
	CurrentLineage() (Lineage, error)
	Checkpoint(context.Context, string) error
	Inspect(context.Context, string) (Snapshot, error)
	WithRestore(context.Context, string, func(RestoreRecords) error) error
}

type (
	AccessCounts struct{ Sessions, Links, Launches int64 }
	ImportCounts struct{ BIOS, Pegasus, EmulationStation int64 }
)

type FenceCounts struct {
	Sessions         int64 `json:"revokedSessionCount"`
	Links            int64 `json:"revokedAccountLinkCount"`
	Launches         int64 `json:"revokedLaunchCount"`
	BIOS             int64 `json:"failedServerImportCount"`
	Pegasus          int64 `json:"failedPegasusJobCount"`
	EmulationStation int64 `json:"failedEmulationStationJobCount"`
}

type FenceAudit struct {
	ID     string
	Now    int64
	Counts FenceCounts
}

type RestoreRecords interface {
	Imports() RestoredImportScope
	RevokeAccess(context.Context, int64) (AccessCounts, error)
	StopExternalImports(context.Context, int64) (ImportCounts, error)
	StopBulkApprovals(context.Context, int64) error
	Audit(context.Context, FenceAudit) error
}
