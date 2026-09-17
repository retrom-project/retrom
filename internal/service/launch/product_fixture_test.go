package launch

import (
	"context"
	"encoding/json"
	model "retrom/internal/model/launch"
	"testing"
	"time"

	"retrom/internal/capability/content/contentcapability"
	"retrom/internal/capability/runtime/runtimebundle"
)

type productTestRepository struct {
	before, current                                                                     model.ProductSnapshot
	receipt, finalReceipt                                                               *model.ProductReceipt
	replayErr, finalReplayErr, snapshotErr, currentErr, writeErr, receiptErr, commitErr error
	inTransaction                                                                       bool
	transactions, loads                                                                 int
	writes                                                                              []model.ProductCreatePlan
	receipts                                                                            []model.ProductReceipt
	variants                                                                            []model.ProductVariantWrite
	pending                                                                             []string
	jobs                                                                                validationJobMemory
	afterCommit                                                                         func()
}

func (repository *productTestRepository) Replay(context.Context, model.ProductCreateCommand) (model.ProductReceipt, bool, error) {
	receipt, err := repository.receipt, repository.replayErr
	if repository.inTransaction {
		receipt, err = repository.finalReceipt, repository.finalReplayErr
	}
	if receipt == nil {
		return model.ProductReceipt{}, false, err
	}
	return *receipt, true, err
}

func (repository *productTestRepository) Snapshot(context.Context, model.ProductCreateCommand) (model.ProductSnapshot, error) {
	if repository.inTransaction {
		return repository.current, repository.currentErr
	}
	repository.loads++
	return repository.before, repository.snapshotErr
}

func (repository *productTestRepository) WithCreation(_ context.Context, work func(model.ProductCreationScope) error) error {
	repository.transactions++
	repository.inTransaction = true
	err := work(repository)
	repository.inTransaction = false
	if err != nil {
		return err
	}
	if repository.commitErr != nil {
		return repository.commitErr
	}
	if repository.afterCommit != nil {
		repository.afterCommit()
	}
	return nil
}

func (repository *productTestRepository) Create(_ context.Context, plan model.ProductCreatePlan) error {
	repository.writes = append(repository.writes, plan)
	return repository.writeErr
}

func (repository *productTestRepository) StoreReceipt(_ context.Context, command model.ProductCreateCommand, receipt model.ProductReceipt) error {
	receipt.Digest = command.Digest
	repository.receipts = append(repository.receipts, receipt)
	return repository.receiptErr
}
func (repository *productTestRepository) Validation() model.ProductValidationScope { return repository }
func (repository *productTestRepository) Find(ctx context.Context, key string) (model.ValidationJob, bool, error) {
	return repository.jobs.Find(ctx, key)
}

func (repository *productTestRepository) Write(ctx context.Context, plan model.ValidationJobWrite) error {
	return repository.jobs.Write(ctx, plan)
}

func (repository *productTestRepository) CreateVariant(_ context.Context, plan model.ProductVariantWrite) error {
	repository.variants = append(repository.variants, plan)
	return repository.writeErr
}

func (repository *productTestRepository) MarkPending(_ context.Context, id string, _ int64) error {
	repository.pending = append(repository.pending, id)
	return repository.writeErr
}

func cloneProductSnapshot(t *testing.T, source model.ProductSnapshot) model.ProductSnapshot {
	t.Helper()
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var result model.ProductSnapshot
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func productFixture(t *testing.T) (*ProductCreator, *productTestRepository, *previewTestProvider, model.ProductCreateCommand) {
	t.Helper()
	source := model.ProductSource{
		GameID: "game", InstanceID: "instance", PlatformID: "platform", CoreID: "core", BindingID: "binding",
		ProviderID: "provider", TargetID: "target", BundleSHA256: "bundle", DeliveryProfile: "ROM_BLOB", ContentKind: "SINGLE_FILE",
		ContentLogicalName: "game.bin", ValidationLogicalName: "game.bin", SourceManifestDigest: "frozen-content", GameVersion: 1,
		VariantID: previewTestID, VariantStatus: "READY", DependencySnapshot: "{}", CompatibilityCode: "READY", ContentPolicy: contentcapability.NewPolicy("SINGLE_FILE"),
	}
	before := model.ProductSnapshot{Found: true, Source: source, GameFiles: []model.ProductFile{{Role: "CONTENT", BlobID: "content", LogicalName: "game.bin", Digest: "content-digest", SizeBytes: 8}}}
	repository := &productTestRepository{before: before, current: cloneProductSnapshot(t, before)}
	provider := &previewTestProvider{target: runtimebundle.Target{}}
	provider.before = func() {
		if repository.inTransaction {
			t.Fatal("provider lookup entered product writer")
		}
	}
	environment := model.ProductEnvironment{Now: func() time.Time { return time.UnixMilli(1000) }, NewID: func() (string, error) { return previewTestID, nil }, SignCapability: func(string) (string, []byte, error) { return "private-cookie-material", make([]byte, 32), nil }}
	command := model.ProductCreateCommand{ProfileID: "profile", ActorID: "actor", Key: "key", Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Request: model.CreateRequest{GameID: "game", ReturnTo: "/games/game"}}
	return NewProductCreator(repository, provider, nil, environment), repository, provider, command
}
