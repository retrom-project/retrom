package libraryimport

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	model "retrom/internal/model/libraryimport"
)

type SourceOwnership struct{ now func() time.Time }

func NewSourceOwnership(now func() time.Time) *SourceOwnership { return &SourceOwnership{now: now} }

func (service *SourceOwnership) Prepare(
	ctx context.Context,
	records model.SourceOwnershipRecords,
	intent model.SourceCreationIntent,
	target string,
) (model.SourceCreationSnapshot, error) {
	if !intent.Kind.Valid() || intent.ImportID == "" || intent.ItemID == "" ||
		intent.JobID == "" || intent.WorkerID == "" ||
		intent.ExecutionNo < 1 || intent.Attempt < 1 || len(intent.PrimaryPaths) == 0 || target == "" {
		return model.SourceCreationSnapshot{}, model.ErrInvalid
	}
	current, err := records.ReadSource(ctx, intent)
	if err != nil {
		return model.SourceCreationSnapshot{}, fmt.Errorf("read source creation ownership: %w", err)
	}
	if !validSourceExecution(current, intent, service.now().UnixMilli()) ||
		current.TargetPlatformInstanceID != target || !sameSourcePaths(current.PrimaryPaths, intent.PrimaryPaths) {
		return model.SourceCreationSnapshot{}, model.ErrVersionConflict
	}
	return current, nil
}

func validSourceExecution(current model.SourceCreationSnapshot, intent model.SourceCreationIntent, now int64) bool {
	return sameSourceExecution(current, intent) &&
		current.ImportState == "RUNNING" && current.JobState == "RUNNING" && current.SourceState == "COPYING" &&
		current.MappingAction == "IMPORT" && current.LeaseUntilMS > now && current.DeadlineMS > now &&
		validSourceVersions(current) && validSourceFrozen(current) &&
		current.LibraryJobID == "" && current.LibraryItemID == ""
}

func validSourceVersions(current model.SourceCreationSnapshot) bool {
	return current.SourceVersion > 0 && current.SourceVersion < math.MaxInt64 && current.ImportVersion > 0 &&
		current.ImportVersion < math.MaxInt64 && current.JobVersion > 0 && current.JobVersion < math.MaxInt64 &&
		current.TargetVersion > 0
}

func (service *SourceOwnership) Revalidate(
	ctx context.Context,
	records model.SourceOwnershipRecords,
	intent model.SourceCreationIntent,
	before model.SourceCreationSnapshot,
	target string,
) (model.SourceCreationSnapshot, error) {
	current, err := service.Prepare(ctx, records, intent, target)
	if err != nil {
		return model.SourceCreationSnapshot{}, err
	}
	if current.Frozen != before.Frozen || !slices.Equal(current.Files, before.Files) {
		return model.SourceCreationSnapshot{}, model.ErrVersionConflict
	}
	if current.SourceVersion != before.SourceVersion || current.TargetVersion != before.TargetVersion ||
		current.TargetPlatformID != before.TargetPlatformID || current.TargetDefaultCoreID != before.TargetDefaultCoreID ||
		current.TargetProviderID != before.TargetProviderID || current.TargetID != before.TargetID ||
		current.TargetDATVersionID != before.TargetDATVersionID {
		return model.SourceCreationSnapshot{}, model.ErrVersionConflict
	}
	return current, nil
}

func (service *SourceOwnership) Attach(
	ctx context.Context,
	records model.SourceOwnershipRecords,
	before model.SourceCreationSnapshot,
	created model.ServerCreated,
	item model.ServerImportItem,
) error {
	if created.ImportJobID == "" || item.ItemID == "" {
		return model.ErrInvalid
	}
	change := model.SourceBindingChange{Before: before, Created: created, Item: item, NowMS: service.now().UnixMilli()}
	if change.NowMS >= before.LeaseUntilMS || change.NowMS >= before.DeadlineMS {
		return model.ErrVersionConflict
	}
	if err := records.BindSource(ctx, change); err != nil {
		return fmt.Errorf("bind created source: %w", err)
	}
	return nil
}

// One server source owns one primary group. Dependency files remain within it.
func ValidateOwnedSourceGroups(primaryPaths []string, groups [][]string) error {
	if len(primaryPaths) == 0 || len(groups) != 1 || !sameSourcePaths(primaryPaths, groups[0]) {
		return fmt.Errorf("%w: %w", model.ErrInvalid, model.ErrSourceGrouping)
	}
	return nil
}

func ValidateOwnedSourceReplayPaths(requested, persisted []string) error {
	if len(persisted) == 0 {
		return model.ErrVersionConflict
	}
	if len(requested) == 0 || !sameSourcePaths(requested, persisted) {
		return model.ErrInvalid
	}
	return nil
}

func sameSourcePaths(left, right []string) bool {
	a, b := slices.Clone(left), slices.Clone(right)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

// Only the declared primary inputs must match the copied source records.
// Additional files may be resolved dependencies inside the selected group.
func ValidateOwnedSourceFiles(snapshot model.SourceCreationSnapshot, inputs []model.ServerSourceFile) error {
	if len(snapshot.Files) == 0 {
		return model.ErrVersionConflict
	}
	byPath := make(map[string]model.ServerSourceFile, len(inputs))
	for _, file := range inputs {
		byPath[file.RelativePath] = file
	}
	for _, source := range snapshot.Files {
		if source.State != "COPIED" || source.File.BlobID == "" || source.File.SizeBytes < 0 ||
			byPath[source.File.RelativePath] != source.File {
			return model.ErrVersionConflict
		}
	}
	return nil
}

func sameSourceExecution(current model.SourceCreationSnapshot, intent model.SourceCreationIntent) bool {
	return current.Kind == intent.Kind && current.ImportID == intent.ImportID && current.ItemID == intent.ItemID &&
		current.JobID == intent.JobID && current.WorkerID == intent.WorkerID && current.ExecutionNo == intent.ExecutionNo &&
		current.Attempt == intent.Attempt
}
