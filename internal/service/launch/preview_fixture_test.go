package launch

import (
	"context"
	"testing"
	"time"

	model "retrom/internal/model/launch"

	"retrom/internal/capability/runtime/runtimebundle"
)

const previewTestID = "01a00000-0000-7000-8000-000000000001"

type previewTestRepository struct {
	snapshot                                                                            model.PreviewSnapshot
	current                                                                             model.PreviewSource
	restore                                                                             model.PreviewRestore
	receipt, finalReceipt                                                               *model.PreviewReceipt
	replayErr, finalReplayErr, snapshotErr, currentErr, restoreErr, writeErr, commitErr error
	missingSnapshot, missingCurrent, missingRestore                                     bool
	replayCount                                                                         int
	loads                                                                               int
	writes                                                                              []model.PreviewCreatePlan
}

func (repository *previewTestRepository) LoadPreviewReplay(_ context.Context, _, _ string) (model.PreviewReceipt, bool, error) {
	repository.replayCount++
	receipt, cause := repository.receipt, repository.replayErr
	if repository.replayCount > 1 {
		receipt, cause = repository.finalReceipt, repository.finalReplayErr
	}
	if receipt == nil {
		return model.PreviewReceipt{}, false, cause
	}
	return *receipt, true, cause
}

func (repository *previewTestRepository) LoadPreviewSnapshot(_ context.Context, _ string) (model.PreviewSnapshot, bool, error) {
	repository.loads++
	return repository.snapshot, !repository.missingSnapshot, repository.snapshotErr
}

func (repository *previewTestRepository) LoadPreviewCurrent(_ context.Context, _ model.ReviewPreviewRequest) (model.PreviewSource, string, bool, error) {
	return repository.current, "profile", !repository.missingCurrent, repository.currentErr
}

func (repository *previewTestRepository) LoadPreviewRestore(_ context.Context, _ string) (model.PreviewRestore, bool, error) {
	return repository.restore, !repository.missingRestore, repository.restoreErr
}

func (repository *previewTestRepository) CommitPreviewCreation(_ context.Context, plan model.PreviewCreatePlan) error {
	repository.writes = append(repository.writes, plan)
	if repository.writeErr != nil {
		return repository.writeErr
	}
	return repository.commitErr
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
func previewFixture(t *testing.T) (*PreviewCreator, *previewTestRepository, *previewTestProvider, model.ReviewPreviewRequest) {
	t.Helper()
	dat := "dat"
	source := model.PreviewSource{
		SourceSnapshotID: "source", PlatformInstanceID: "instance", ProviderID: "provider", TargetID: "target",
		BundleSHA256: "bundle", CoreID: "core", DeliveryProfile: "ROM_BLOB", ContentKind: "SINGLE_FILE", DATVersionID: &dat,
		ValidationID: "validation", ValidationStatus: "READY", DependencySnapshot: "frozen",
	}
	repository := &previewTestRepository{snapshot: model.PreviewSnapshot{Source: source, SourceFiles: []model.PreviewFile{{Role: "CONTENT", LogicalName: "game.bin", BlobID: "game"}}}, current: source}
	provider := &previewTestProvider{target: runtimebundle.Target{Inputs: []runtimebundle.Input{{Role: "game"}}}}
	environment := model.PreviewEnvironment{
		Now: func() time.Time { return time.UnixMilli(1000) }, NewID: func() (string, error) { return previewTestID, nil },
		SignCapability: func(string) (string, []byte, error) { return "test", make([]byte, 32), nil },
	}
	return NewPreviewCreator(repository, provider, environment), repository, provider, model.ReviewPreviewRequest{ImportItemID: "item", ActorUserID: "actor", IdempotencyKey: "key"}
}

func previewFixtureRestore(repository *previewTestRepository) model.PreviewRestore {
	return model.PreviewRestore{
		ActorID: "actor", ItemID: "item", SnapshotID: "source", ProviderID: "provider", TargetID: "target",
		State: "ACTIVE", HardExpiresAtMS: 2000, ContentBlobID: "game", ContentName: "game.bin", ContentFormat: "SOURCE_V1",
		DependencySnapshot: repository.current.DependencySnapshot, BlobID: "saved-B", Format: "checkpoint-v1", SizeBytes: 100,
		MaximumBytes: 100, ReadFormats: []string{"checkpoint-v1"},
	}
}
