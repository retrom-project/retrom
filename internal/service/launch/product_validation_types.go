package launch

import (
	"context"
	"time"

	"retrom/internal/capability/content/contentcapability"
)

// ValidationInputs is the immutable VARIANT_VALIDATE input shared with the worker.
type ValidationInputs struct {
	GameID                string                   `json:"gameId"`
	GameVariantID         string                   `json:"gameVariantId"`
	GameVersion           int64                    `json:"gameVersion"`
	SourceManifestDigest  string                   `json:"sourceManifestDigest"`
	ProviderID            string                   `json:"providerId"`
	TargetID              string                   `json:"targetId"`
	ContentPolicy         contentcapability.Policy `json:"contentPolicy"`
	DATVersionID          *string                  `json:"datVersionId"`
	ValidationInputDigest string                   `json:"validationInputDigest"`
	BIOSDependencyDigest  string                   `json:"biosDependencyDigest"`
}
type ValidationSnapshot struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          string           `json:"kind"`
	Scope         ValidationScope  `json:"scope"`
	ExecutionID   string           `json:"executionId"`
	Inputs        ValidationInputs `json:"inputs"`
}
type ValidationScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type ValidationJob struct {
	ID, State, SnapshotJSON string
	Retryable               bool
	ExecutionNo, Version    int64
}
type ValidationJobWrite struct {
	JobID, VariantID, DedupeKey            string
	SnapshotJSON, PayloadJSON, InputDigest string
	ExecutionNo, PreviousVersion, NowMS    int64
	Retry                                  bool
}
type ValidationQueued struct {
	JobID  string
	Queued bool
}
type ValidationJobRepository interface {
	Find(context.Context, string) (ValidationJob, bool, error)
	Write(context.Context, ValidationJobWrite) error
}
type ValidationEnvironment struct {
	Now   func() time.Time
	NewID func() (string, error)
}
