package launch

import (
	"context"
	"testing"
	"time"

	"retrom/internal/runtimebundle"
)

const previewTestID = "01a00000-0000-7000-8000-000000000001"

type previewTestRepository struct {
	snapshot                                                                            PreviewSnapshot
	current                                                                             PreviewSource
	restore                                                                             PreviewRestore
	receipt, finalReceipt                                                               *PreviewReceipt
	replayErr, finalReplayErr, snapshotErr, currentErr, restoreErr, writeErr, commitErr error
	missingSnapshot, missingCurrent, missingRestore                                     bool
	inTransaction                                                                       bool
	transactions, loads                                                                 int
	writes                                                                              []PreviewCreatePlan
}

func (repository *previewTestRepository) Replay(context.Context, string, string) (PreviewReceipt, bool, error) {
	receipt, cause := repository.receipt, repository.replayErr
	if repository.inTransaction {
		receipt, cause = repository.finalReceipt, repository.finalReplayErr
	}
	if receipt == nil {
		return PreviewReceipt{}, false, cause
	}
	return *receipt, true, cause
}

func (repository *previewTestRepository) Snapshot(context.Context, string) (PreviewSnapshot, bool, error) {
	repository.loads++
	return repository.snapshot, !repository.missingSnapshot, repository.snapshotErr
}

func (repository *previewTestRepository) WithCreation(_ context.Context, work func(PreviewCreationScope) error) error {
	repository.transactions++
	repository.inTransaction = true
	defer func() { repository.inTransaction = false }()
	if err := work(repository); err != nil {
		return err
	}
	return repository.commitErr
}

func (repository *previewTestRepository) Current(context.Context, ReviewPreviewRequest) (PreviewSource, string, bool, error) {
	return repository.current, "profile", !repository.missingCurrent, repository.currentErr
}

func (repository *previewTestRepository) Restore(context.Context, string) (PreviewRestore, bool, error) {
	return repository.restore, !repository.missingRestore, repository.restoreErr
}

func (repository *previewTestRepository) Create(_ context.Context, plan PreviewCreatePlan) error {
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
func previewFixture(t *testing.T) (*PreviewCreator, *previewTestRepository, *previewTestProvider, ReviewPreviewRequest) {
	t.Helper()
	dat := "dat"
	source := PreviewSource{
		SourceSnapshotID: "source", PlatformInstanceID: "instance", ProviderID: "provider", TargetID: "target",
		BundleSHA256: "bundle", CoreID: "core", DeliveryProfile: "ROM_BLOB", ContentKind: "SINGLE_FILE", DATVersionID: &dat,
		ValidationID: "validation", ValidationStatus: "READY", DependencySnapshot: "frozen",
	}
	repository := &previewTestRepository{snapshot: PreviewSnapshot{Source: source, SourceFiles: []PreviewFile{{Role: "CONTENT", LogicalName: "game.bin", BlobID: "game"}}}, current: source}
	provider := &previewTestProvider{target: runtimebundle.Target{Inputs: []runtimebundle.Input{{Role: "game"}}}}
	provider.before = func() {
		if repository.inTransaction {
			t.Fatal("provider called inside creation transaction")
		}
	}
	environment := PreviewEnvironment{
		Now: func() time.Time { return time.UnixMilli(1000) }, NewID: func() (string, error) { return previewTestID, nil },
		SignCapability: func(string) (string, []byte, error) { return "test", make([]byte, 32), nil },
	}
	return NewPreviewCreator(repository, provider, environment), repository, provider, ReviewPreviewRequest{ImportItemID: "item", ActorUserID: "actor", IdempotencyKey: "key"}
}

func previewFixtureRestore(repository *previewTestRepository) PreviewRestore {
	return PreviewRestore{
		ActorID: "actor", ItemID: "item", SnapshotID: "source", ProviderID: "provider", TargetID: "target",
		State: "ACTIVE", HardExpiresAtMS: 2000, ContentBlobID: "game", ContentName: "game.bin", ContentFormat: "SOURCE_V1",
		DependencySnapshot: repository.current.DependencySnapshot, BlobID: "saved-B", Format: "checkpoint-v1", SizeBytes: 100,
		MaximumBytes: 100, ReadFormats: []string{"checkpoint-v1"},
	}
}
