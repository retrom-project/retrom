package pegasusimport

import (
	"context"
	"fmt"
	"math"
	"time"

	payload "retrom/internal/service/payloadrelease"
)

type (
	ExecutionItem struct {
		ID, ImportID, State                                                    string
		Version                                                                int64
		TargetPlatformID, TargetPlatformKind, TargetDATVersionID, MetadataJSON string
		LibraryImportJobID, LibraryImportItemID                                string
		TagIDs                                                                 []string
		Files                                                                  []ExecutionFile
		Assets                                                                 []ExecutionAsset
	}
	ExecutionFile struct {
		Ordinal     int64
		Path, Facts string
		Size        int64
		BlobID      string
	}
	ExecutionAsset struct {
		Kind, Path, Facts, MediaType string
		Size                         int64
		Width, Height                *int64
		BlobID                       string
	}
	OwnedItem struct {
		Execution ExecutionSnapshot
		Item      ExecutionItem
	}
	ItemOutcome struct {
		State, Code, ExistingGameID string
		Retryable                   bool
		Failure                     *FailureDetails
		ExistingMatches             []ExistingMatch
	}
	ItemClaim struct {
		Before OwnedItem
		NowMS  int64
	}
	ItemResume struct {
		Before OwnedItem
		NowMS  int64
	}
	ItemFinish struct {
		Before  OwnedItem
		Outcome ItemOutcome
		NowMS   int64
	}
	ItemWorkReader interface {
		Execution(context.Context, string) (ExecutionSnapshot, error)
		Next(context.Context, string) (ExecutionItem, bool, error)
		Current(context.Context, string) (OwnedItem, error)
	}
	ItemWorkWriter interface {
		Claim(context.Context, ItemClaim) error
		Resume(context.Context, ItemResume) error
		Finish(context.Context, ItemFinish) error
	}
	ItemWorkScope struct {
		Payload payload.ReleaseScope
		Read    ItemWorkReader
		Write   ItemWorkWriter
	}
	ItemWorkRepository interface {
		WithItemWork(context.Context, func(ItemWorkScope) error) error
	}
	ItemWork struct {
		repository ItemWorkRepository
		now        func() time.Time
	}
)

func NewItemWork(repository ItemWorkRepository, now func() time.Time) *ItemWork {
	return &ItemWork{repository: repository, now: now}
}

func (service *ItemWork) Next(ctx context.Context, identity ExecutionIdentity) (ExecutionItem, bool, error) {
	var item ExecutionItem
	found := false
	err := service.repository.WithItemWork(ctx, func(scope ItemWorkScope) error {
		execution, err := scope.Read.Execution(ctx, identity.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus item execution: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(execution, identity, now); err != nil {
			return err
		}
		if execution.Kind != "SERVER_PEGASUS_IMPORT" || execution.JobState != "RUNNING" {
			return ErrVersionConflict
		}
		item, found, err = scope.Read.Next(ctx, identity.ImportID)
		if err != nil {
			return fmt.Errorf("read next Pegasus item: %w", err)
		}
		if !found {
			return nil
		}
		if item.ImportID != identity.ImportID || item.State != "PENDING" || !validItemVersion(item.Version) {
			return ErrVersionConflict
		}
		if err := scope.Write.Claim(
			ctx,
			ItemClaim{Before: OwnedItem{Execution: execution, Item: item}, NowMS: now},
		); err != nil {
			return fmt.Errorf("claim Pegasus item: %w", err)
		}
		item.State, item.Version = "COPYING", item.Version+1
		return nil
	})
	if err != nil {
		return ExecutionItem{}, false, fmt.Errorf("begin Pegasus item work: %w", err)
	}
	return item, found, nil
}

func (service *ItemWork) Resume(
	ctx context.Context,
	identity ExecutionIdentity,
	itemID, jobID, libraryItemID string,
) error {
	err := service.repository.WithItemWork(ctx, func(scope ItemWorkScope) error {
		before, now, err := service.owned(ctx, scope.Read, identity, itemID)
		if err != nil {
			return err
		}
		if jobID == "" || libraryItemID == "" || before.Item.LibraryImportJobID != jobID ||
			before.Item.LibraryImportItemID != libraryItemID {
			return ErrVersionConflict
		}
		if before.Item.State == "VALIDATING" || before.Item.State == "REVIEW_PENDING" {
			return nil
		}
		if before.Item.State != "COPYING" {
			return ErrVersionConflict
		}
		if err := scope.Write.Resume(ctx, ItemResume{Before: before, NowMS: now}); err != nil {
			return fmt.Errorf("resume bound Pegasus review: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("resume Pegasus item: %w", err)
	}
	return nil
}

func (service *ItemWork) Finish(
	ctx context.Context,
	identity ExecutionIdentity,
	itemID string,
	outcome ItemOutcome,
) error {
	if !validItemOutcome(outcome) {
		return ErrInvalid
	}
	err := service.repository.WithItemWork(ctx, func(scope ItemWorkScope) error {
		before, now, err := service.owned(ctx, scope.Read, identity, itemID)
		if err != nil {
			return err
		}
		if before.Item.State == outcome.State {
			return nil
		}
		if before.Item.State != "COPYING" && before.Item.State != "VALIDATING" {
			return ErrVersionConflict
		}
		if err := scope.Write.Finish(ctx, ItemFinish{Before: before, Outcome: outcome, NowMS: now}); err != nil {
			return fmt.Errorf("save Pegasus item outcome: %w", err)
		}
		_, err = payload.NewScheduler(nil).TerminalSource(
			ctx, scope.Payload.Scheduling,
			payload.Scope{Type: payload.ScopePegasusImportItem, ID: itemID}, now,
		)
		if err != nil {
			return fmt.Errorf("schedule Pegasus item payloads: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("finish Pegasus item: %w", err)
	}
	return nil
}

func (service *ItemWork) owned(
	ctx context.Context,
	reader ItemWorkReader,
	identity ExecutionIdentity,
	id string,
) (OwnedItem, int64, error) {
	before, err := reader.Current(ctx, id)
	if err != nil {
		return OwnedItem{}, 0, fmt.Errorf("read Pegasus item ownership: %w", err)
	}
	now := service.now().UnixMilli()
	if err := ValidateExecution(before.Execution, identity, now); err != nil {
		return OwnedItem{}, 0, err
	}
	if before.Item.ID != id || before.Item.ImportID != identity.ImportID || !validItemVersion(before.Item.Version) {
		return OwnedItem{}, 0, ErrVersionConflict
	}
	return before, now, nil
}

func validItemVersion(version int64) bool { return version > 0 && version < math.MaxInt64 }

func validItemOutcome(outcome ItemOutcome) bool {
	switch outcome.State {
	case "SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED", "BLOCKED_CONTENT", "CANCELLED":
		return outcome.Code != ""
	case "SKIPPED_EXISTING":
		return outcome.ExistingGameID != "" && len(outcome.ExistingMatches) > 0
	default:
		return false
	}
}
