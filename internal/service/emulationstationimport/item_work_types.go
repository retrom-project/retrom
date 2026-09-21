package emulationstationimport

import (
	"context"
	"time"

	payload "retrom/internal/service/payloadrelease"
)

type ExecutionItem struct {
	ID, ImportID, State                                                                 string
	Version                                                                             int64
	TargetPlatformID, TargetPlatformKind, TargetDATVersionID, MetadataJSON, ContentKind string
	LibraryImportJobID, LibraryImportItemID                                             string
	TagIDs                                                                              []string
	Files                                                                               []ExecutionFile
	Assets                                                                              []ExecutionAsset
}
type ExecutionFile struct {
	Ordinal                    int64
	Path, Facts, State, BlobID string
	Size                       int64
}
type ExecutionAsset struct {
	Kind, Path, Facts, MediaType, State, BlobID string
	Size                                        int64
	Width, Height                               *int64
}
type OwnedItem struct {
	Execution LeaseSnapshot
	Item      ExecutionItem
}
type ItemOutcome struct {
	State, Code, ExistingGameID string
	Retryable                   bool
	Failure                     *FailureDetails
	ExistingMatches             []ExistingMatch
}
type ItemClaim struct {
	Before OwnedItem
	NowMS  int64
}
type ItemResume struct {
	Before OwnedItem
	NowMS  int64
}
type ItemFinish struct {
	Before  OwnedItem
	Outcome ItemOutcome
	NowMS   int64
}
type ItemWorkReader interface {
	Current(context.Context, string) (LeaseSnapshot, bool, error)
	Next(context.Context, string) (ExecutionItem, bool, error)
	Item(context.Context, string) (OwnedItem, error)
}
type ItemWorkWriter interface {
	Claim(context.Context, ItemClaim) error
	Resume(context.Context, ItemResume) error
	Finish(context.Context, ItemFinish) error
}
type ItemWorkScope struct {
	Payload payload.ReleaseScope
	Read    ItemWorkReader
	Write   ItemWorkWriter
}
type ItemWorkRepository interface {
	WithItemWork(context.Context, func(ItemWorkScope) error) error
}
type ItemWork struct {
	repository ItemWorkRepository
	now        func() time.Time
}

func NewItemWork(repository ItemWorkRepository, now func() time.Time) *ItemWork {
	return &ItemWork{repository: repository, now: now}
}
