package libraryimport

import (
	"context"

	"retrom/internal/model/importprogress"
)

type ApprovalMetadata struct {
	Title, Description, Developer, Publisher, Genre string
	Players, ReleaseYear                            *int
}

type ApprovalGame struct {
	ID, PlatformInstanceID, TitleInitial, SearchText string
	SourceKind, SourceRefID, ContentKind             string
	ManifestJSON, ManifestDigest                     string
	Metadata                                         ApprovalMetadata
	NowMS                                            int64
}

type ApprovalAsset struct {
	ID, GameID string
	ApprovalExternalAsset
	Ordinal int
	NowMS   int64
}

type ApprovalContentCopy struct {
	GameID, ItemID, SnapshotID, DraftID string
	NowMS                               int64
}

type ApprovalVariant struct {
	ID, GameID, CoreID, ProviderID, TargetID string
	DATID, DefaultDOS                        *string
	EmulatorGameID                           *int64
	CompatibilityCode, DependencyJSON        string
	NowMS                                    int64
}

type (
	ApprovalValidationCopy    struct{ VariantID, ValidationID string }
	ApprovalRPGVariant        struct{ VariantID, Generation, DependencyDigest string }
	ApprovalVariantDependency struct {
		VariantID, Kind, Machine, DATID, RequiredEntriesJSON, State string
		NowMS                                                       int64
	}
)

type ApprovalEvent struct {
	ID, ItemID, ActorKind                                              string
	ActorUserID, ActorLabel, Reason                                    *string
	BeforeJSON, AfterJSON, DiffJSON, ConfigJSON, DATJSON, ProviderJSON string
	NowMS                                                              int64
}

type ApprovalPublication struct {
	ItemID, ImportID, SnapshotID, PlatformInstanceID             string
	ExpectedDraftVersion, ExpectedParentVersion, ExpectedPending int64
	Projection                                                   importprogress.Projection
	NowMS                                                        int64
}

type BulkPublication struct {
	Intent                       BulkPublicationIntent
	ItemID                       string
	Result                       ReviewApproved
	NowMS                        int64
	ReviewVersion, LeasedUntilMS int64
}

type ApprovalGameWriter interface {
	CreateGame(context.Context, ApprovalGame) error
	CopySourceFiles(context.Context, ApprovalContentCopy) error
	CopyRPGProfile(context.Context, ApprovalContentCopy) error
	CreateAsset(context.Context, ApprovalAsset) error
	CopyDOSEntries(context.Context, ApprovalContentCopy) error
}

type ApprovalVariantWriter interface {
	NextEmulatorID(context.Context) (int64, error)
	CreateVariant(context.Context, ApprovalVariant) error
	CopyValidationFiles(context.Context, ApprovalValidationCopy) error
	CreateDependency(context.Context, ApprovalVariantDependency) error
	CreateRPGVariant(context.Context, ApprovalRPGVariant) error
}

type ApprovalDecisionWriter interface {
	ClaimIdentity(context.Context, string, string, int64) error
	RecordEvent(context.Context, ApprovalEvent) error
	PublishItem(context.Context, ApprovalPublication) error
	TransitionOwner(context.Context, ReviewOwnerTransition) error
}

type BulkPublicationWriter interface {
	RecordPublished(context.Context, BulkPublication) error
}
