package sourceimport

import (
	"context"
	"time"

	"retrom/internal/filestore"
)

type (
	MaterialFiles interface {
		CopyTo(context.Context, string, string, string) (filestore.Metadata, error)
	}
	MaterialKey struct {
		ItemID, Kind string
		Ordinal      int64
	}
	MaterialSource struct {
		Key                    MaterialKey
		Path, Facts, MediaType string
		Size                   int64
		Width, Height          *int64
	}
	VerifiedBlob struct {
		ID, SHA256, MD5, SHA1, CRC32, StoragePath string
		Size                                      int64
	}
	MaterialSnapshot struct {
		Before                         OwnedItem
		Source                         MaterialSource
		State, FileRecord, WarningCode string
		Blob                           VerifiedBlob
		Warnings                       []map[string]any
	}
	MaterialBinding struct {
		Before MaterialSnapshot
		Blob   VerifiedBlob
		NowMS  int64
	}
	MaterialWarning struct {
		Before      MaterialSnapshot
		State, Code string
		Warnings    []map[string]any
		NowMS       int64
	}
	ExecutionPhase struct {
		Execution ExecutionSnapshot
		Phase     string
	}
	PhaseChange struct {
		Before ExecutionPhase
		Phase  string
		NowMS  int64
	}
	MaterialReader interface {
		Source(context.Context, MaterialKey) (MaterialSnapshot, error)
		Execution(context.Context, string) (ExecutionPhase, error)
	}
	MaterialWriter interface {
		Bind(context.Context, MaterialBinding) (string, error)
		Warn(context.Context, MaterialWarning) error
		Phase(context.Context, PhaseChange) error
	}
	MaterialScope struct {
		Read  MaterialReader
		Write MaterialWriter
	}
	MaterialRepository interface {
		WithMaterialization(context.Context, func(MaterialScope) error) error
	}
	Materialization struct {
		repository MaterialRepository
		files      MaterialFiles
		now        func() time.Time
	}
)

func NewMaterialization(repository MaterialRepository, files MaterialFiles, now func() time.Time) *Materialization {
	return &Materialization{repository: repository, files: files, now: now}
}
