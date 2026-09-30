package libraryimport

import (
	"context"
	"testing"
	"time"

	launch "retrom/internal/service/launch"

	runtimebundle "retrom/internal/runtime/bundle"
)

const previewTestID = "01a00000-0000-7000-8000-000000000001"

type previewTestRepository struct {
	snapshot                                                                            launch.PreviewSnapshot
	current                                                                             launch.PreviewSource
	currentSnapshot                                                                     *launch.PreviewSnapshot
	restore                                                                             launch.PreviewRestore
	receipt, finalReceipt                                                               *launch.PreviewReceipt
	replayErr, finalReplayErr, snapshotErr, currentErr, restoreErr, writeErr, commitErr error
	missingSnapshot, missingCurrent, missingRestore                                     bool
	inTransaction                                                                       bool
	transactions, loads                                                                 int
	writes                                                                              []launch.PreviewCreatePlan
}

func (repository *previewTestRepository) Replay(context.Context, string, string) (launch.PreviewReceipt, bool, error) {
	receipt, cause := repository.receipt, repository.replayErr
	if repository.inTransaction {
		receipt, cause = repository.finalReceipt, repository.finalReplayErr
	}
	if receipt == nil {
		return launch.PreviewReceipt{}, false, cause
	}
	return *receipt, true, cause
}

func (repository *previewTestRepository) Snapshot(context.Context, string) (launch.PreviewSnapshot, bool, error) {
	repository.loads++
	return repository.snapshot, !repository.missingSnapshot, repository.snapshotErr
}

func (repository *previewTestRepository) WithCreation(_ context.Context, work func(ReviewPreviewScope) error) error {
	repository.transactions++
	repository.inTransaction = true
	defer func() { repository.inTransaction = false }()
	if err := work(repository); err != nil {
		return err
	}
	return repository.commitErr
}

func (repository *previewTestRepository) Current(context.Context,
	launch.ReviewPreviewRequest,
) (launch.PreviewSnapshot, string, bool, error) {
	if repository.currentSnapshot != nil {
		return *repository.currentSnapshot, "profile", !repository.missingCurrent, repository.currentErr
	}
	return launch.PreviewSnapshot{Source: repository.current, SourceFiles: repository.snapshot.SourceFiles, ValidationFiles: repository.snapshot.ValidationFiles}, "profile", !repository.missingCurrent, repository.currentErr
}

func (repository *previewTestRepository) Restore(context.Context, string) (launch.PreviewRestore, bool, error) {
	return repository.restore, !repository.missingRestore, repository.restoreErr
}

func (repository *previewTestRepository) Create(_ context.Context, plan launch.PreviewCreatePlan) error {
	repository.writes = append(repository.writes, plan)
	return repository.writeErr
}

type previewTestProvider struct {
	target runtimebundle.Target
	absent bool
	before func()
}

func (provider *previewTestProvider) Target(string, string) (runtimebundle.Target, bool) {
	if provider.before != nil {
		provider.before()
	}
	return provider.target, !provider.absent
}
func (*previewTestProvider) BundleSHA256(string, string) (string, bool) { return "bundle", true }
func previewFixture(t *testing.T) (*ReviewPreviews, *previewTestRepository,
	*previewTestProvider, launch.ReviewPreviewRequest,
) {
	t.Helper()
	dat := "dat"
	source := launch.PreviewSource{
		SourceSnapshotID: "source", PlatformInstanceID: "instance", ProviderID: "provider", TargetID: "target",
		BundleSHA256: "bundle", CoreID: "core", DeliveryProfile: "ROM_BLOB", ContentKind: "SINGLE_FILE", DATVersionID: &dat,
		ValidationStatus: "READY", DependencySnapshot: "frozen",
	}
	repository := &previewTestRepository{
		snapshot: launch.PreviewSnapshot{
			Source:      source,
			SourceFiles: []launch.PreviewFile{{Role: "CONTENT", LogicalName: "game.bin", FileRecord: "game"}},
		},
		current: source,
	}
	provider := &previewTestProvider{target: runtimebundle.Target{Inputs: []runtimebundle.Input{{Role: "game"}}}}
	provider.before = func() {
		if repository.inTransaction {
			t.Fatal("provider called inside creation transaction")
		}
	}
	environment := launch.PreviewEnvironment{
		Now: func() time.Time { return time.UnixMilli(1000) }, NewID: func() (string, error) { return previewTestID, nil },
		SignCapability: func(string) (string, []byte, error) { return "test", make([]byte, 32), nil },
		CopyRestorePayload: func(_ context.Context, _, record string) (string, error) {
			if repository.inTransaction {
				t.Fatal("restore copied inside creation transaction")
			}
			return "copy-" + record, nil
		},
		DiscardPreviewPayload: func(context.Context, string) error { return nil },
	}
	return NewReviewPreviews(repository, provider, environment), repository, provider,
		launch.ReviewPreviewRequest{ImportItemID: "item", ActorUserID: "actor", IdempotencyKey: "key"}
}

func previewFixtureRestore(repository *previewTestRepository) launch.PreviewRestore {
	return launch.PreviewRestore{
		ActorID: "actor", ItemID: "item", SnapshotID: "source", ProviderID: "provider", TargetID: "target",
		State: "ACTIVE", HardExpiresAtMS: 2000, ContentFileRecord: "game", ContentName: "game.bin",
		ContentFormat:      "SOURCE_V1",
		DependencySnapshot: repository.current.DependencySnapshot, FileRecord: "saved-B",
		Format: "checkpoint-v1", SizeBytes: 100,
		MaximumBytes: 100, ReadFormats: []string{"checkpoint-v1"},
	}
}
