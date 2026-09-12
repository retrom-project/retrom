package pegasusimport

import (
	"context"
	"time"
)

type (
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
		SHA256, MD5, SHA1, CRC32 string
		Size                     int64
	}
	MaterialSnapshot struct {
		Before                     OwnedItem
		Source                     MaterialSource
		State, BlobID, WarningCode string
		Blob                       VerifiedBlob
		Warnings                   []map[string]any
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
		now        func() time.Time
	}
)

func NewMaterialization(repository MaterialRepository, now func() time.Time) *Materialization {
	return &Materialization{repository: repository, now: now}
}
