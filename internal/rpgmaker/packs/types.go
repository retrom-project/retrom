package packs

import "errors"

var (
	ErrConflict    = errors.New("RPG_RUNTIME_PACK_CONFLICT")
	ErrReferenced  = errors.New("RPG_RUNTIME_PACK_REFERENCED")
	ErrNotFound    = errors.New("RPG_RUNTIME_PACK_NOT_FOUND")
	ErrStale       = errors.New("RPG_RUNTIME_PACK_STALE")
	ErrTooLarge    = errors.New("RPG_RUNTIME_PACK_TOO_LARGE")
	ErrUnavailable = errors.New("RPG_RUNTIME_PACK_UNAVAILABLE")
	errLayoutData  = errors.New("runtime pack layout registry invalid")
	errFinishData  = errors.New("runtime pack finish transition changed no row")
)

type FileIdentity struct {
	Path      string
	BlobID    string
	SizeBytes int64
	SHA256    string
}

type InstallRequest struct {
	UploadID     string  `json:"uploadId"`
	Kind         string  `json:"kind"`
	Generation   *string `json:"generation,omitempty"`
	DeclaredName *string `json:"declaredName,omitempty"`
	SourceNote   *string `json:"sourceNote,omitempty"`
	CreatorID    string  `json:"-"`
}

type InstallAccepted struct {
	InstallationID string `json:"installationId"`
	JobID          string `json:"jobId"`
	Status         string `json:"status"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type ReferenceCounts struct {
	GameCount       int64 `json:"gameCount"`
	CheckpointCount int64 `json:"checkpointCount"`
}

type DefinitionView struct {
	DefinitionID           string `json:"definitionId"`
	Kind                   string `json:"kind"`
	Generation             string `json:"generation"`
	DeclaredName           string `json:"declaredName"`
	NormalizedDeclaredName string `json:"normalizedDeclaredName"`
	DisplayName            string `json:"displayName"`
	RequiredLayout         string `json:"requiredLayoutVersion"`
	Origin                 string `json:"origin"`
	Enabled                bool   `json:"enabled"`
}

type InstallationView struct {
	InstallationID string          `json:"installationId"`
	DefinitionID   string          `json:"definitionId"`
	FilesDigest    string          `json:"filesDigest"`
	FileCount      int64           `json:"fileCount"`
	TotalBytes     int64           `json:"totalBytes"`
	BundleSHA256   *string         `json:"bundleSha256"`
	Status         string          `json:"status"`
	Diagnostics    []Diagnostic    `json:"diagnostics"`
	SourceNote     *string         `json:"sourceNote"`
	References     ReferenceCounts `json:"references"`
	Version        int64           `json:"version"`
	CreatedAtMS    int64           `json:"createdAtMs"`
	ValidatedAtMS  *int64          `json:"validatedAtMs"`
	DeletedAtMS    *int64          `json:"deletedAtMs"`
}

type ListView struct {
	Definitions   []DefinitionView   `json:"definitions"`
	Installations []InstallationView `json:"installations"`
}
