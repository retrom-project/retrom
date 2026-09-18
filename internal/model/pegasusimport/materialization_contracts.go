package pegasusimport

import "context"

type MaterialKey struct {
	ItemID, Kind string
	Ordinal      int64
}

type MaterialSource struct {
	Key                    MaterialKey
	Path, Facts, MediaType string
	Size                   int64
	Width, Height          *int64
}

type VerifiedBlob struct {
	SHA256, MD5, SHA1, CRC32 string
	Size                     int64
}

type MaterialSnapshot struct {
	Before                     OwnedItem
	Source                     MaterialSource
	State, BlobID, WarningCode string
	Blob                       VerifiedBlob
	Warnings                   []map[string]any
}

type MaterialBinding struct {
	Before MaterialSnapshot
	Blob   VerifiedBlob
	NowMS  int64
}

type MaterialWarning struct {
	Before      MaterialSnapshot
	State, Code string
	Warnings    []map[string]any
	NowMS       int64
}

type ExecutionPhase struct {
	Execution ExecutionSnapshot
	Phase     string
}

type PhaseChange struct {
	Before ExecutionPhase
	Phase  string
	NowMS  int64
}

type MaterialRepository interface {
	LoadMaterialSource(context.Context, MaterialKey) (MaterialSnapshot, error)
	LoadExecutionPhase(context.Context, string) (ExecutionPhase, error)
	CommitMaterialBinding(context.Context, MaterialBinding) (string, error)
	CommitMaterialWarning(context.Context, MaterialWarning) error
	CommitPhaseChange(context.Context, PhaseChange) error
}
