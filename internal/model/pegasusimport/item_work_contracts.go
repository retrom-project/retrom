package pegasusimport

import (
	"context"

	payload "retrom/internal/model/payloadrelease"
)

type ExecutionItem struct {
	ID, ImportID, State                                                    string
	Version                                                                int64
	TargetPlatformID, TargetPlatformKind, TargetDATVersionID, MetadataJSON string
	LibraryImportJobID, LibraryImportItemID                                string
	TagIDs                                                                 []string
	Files                                                                  []ExecutionFile
	Assets                                                                 []ExecutionAsset
}

type ExecutionFile struct {
	Ordinal     int64
	Path, Facts string
	Size        int64
	BlobID      string
}

type ExecutionAsset struct {
	Kind, Path, Facts, MediaType string
	Size                         int64
	Width, Height                *int64
	BlobID                       string
}

type OwnedItem struct {
	Execution ExecutionSnapshot
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
	Execution(context.Context, string) (ExecutionSnapshot, error)
	Next(context.Context, string) (ExecutionItem, bool, error)
	Current(context.Context, string) (OwnedItem, error)
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

type ClaimNextItemResult struct {
	Item  ExecutionItem
	Found bool
}

type ItemWorkRepository interface {
	ClaimNextItem(ctx context.Context, unit ExecutionIdentity, nowMS int64) (ClaimNextItemResult, error)
	CommitItemResume(ctx context.Context, unit ExecutionIdentity, itemID, jobID, ordinaryID string, nowMS int64) error
	CommitItemFinish(ctx context.Context, unit ExecutionIdentity, itemID string, outcome ItemOutcome, nowMS int64) error
}
