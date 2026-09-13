package libraryimport

import (
	"context"
	"crypto/sha256"
	"time"

	"retrom/internal/capability/content/multidisc"
)

type MultiDiscAttachmentBaseFiles struct {
	Files   []MultiDiscAttachmentFile
	Entries []multidisc.Entry
}

type MultiDiscAttachmentUploadFiles struct {
	State, SourceType string
	Consumed          bool
	Files             []MultiDiscAttachmentFile
}

type MultiDiscAttachmentSourceRepository interface {
	BaseFiles(context.Context, string) (MultiDiscAttachmentBaseFiles, error)
	UploadFiles(context.Context, string) (MultiDiscAttachmentUploadFiles, error)
}

type MultiDiscAttachmentWorkerClaim struct {
	Input                MultiDiscAttachmentInput
	JobID                string
	WorkerID             string
	ExecutionStartedAtMS int64
}

type MultiDiscAttachmentWorkerRepository interface {
	Claim(context.Context, string, string, int64) (MultiDiscAttachmentWorkerClaim, error)
	Heartbeat(context.Context, string, string, int64) error
}

func ValidMultiDiscAttachmentInput(input MultiDiscAttachmentInput) bool {
	return input.SchemaVersion == 1 && input.AttachmentID != "" && input.ImportItemID != "" &&
		input.RequestedByUserID != "" && input.MaxDiscs >= multidisc.MinDiscs &&
		input.MaxDiscs <= multidisc.MaxDiscs && input.MaxTotalBytes >= 1 &&
		input.ProviderID != "" && input.TargetID != "" &&
		len(input.ContentPolicyDigest) == sha256.Size*2
}

const (
	MultiDiscAttachmentDeadline  = 30 * time.Minute
	MultiDiscAttachmentReadChunk = 8 << 20
)
