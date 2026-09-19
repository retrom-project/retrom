package pegasusimport

import (
	"context"

	libraryimportmodel "retrom/internal/model/libraryimport"
	model "retrom/internal/model/pegasusimport"
)

type importExecutorFixture struct {
	items       []model.ExecutionItem
	events      []string
	failures    map[string]error
	outcomes    []model.ItemOutcome
	reviewFiles []libraryimportmodel.ServerSourceFile
	companions  []model.CompanionCandidate
	claims      int
	resumed     bool
	validMedia  bool
	cancelled   bool
	warning     string
}

func newImportExecutorFixture() (*importExecutorFixture, *ImportExecutor) {
	fake := &importExecutorFixture{
		items:    []model.ExecutionItem{{ID: "item", Files: []model.ExecutionFile{{Path: "game.gba", Size: 4}}}},
		failures: map[string]error{},
	}
	return fake, NewImportExecutor(ImportExecutorDependencies{
		Items: fake, Materials: fake, Sources: fake, Reviews: fake, Companions: fake,
		Settlement: executorCancellationFixture{fake}, Completion: executorCompletionFixture{fake}, Diagnostics: fake,
	})
}

func (fake *importExecutorFixture) record(event string) error {
	fake.events = append(fake.events, event)
	return fake.failures[event]
}

func (fake *importExecutorFixture) Next(context.Context, model.ExecutionIdentity) (model.ExecutionItem, bool, error) {
	fake.claims++
	if err := fake.record("next"); err != nil {
		return model.ExecutionItem{}, false, err
	}
	if len(fake.items) == 0 {
		return model.ExecutionItem{}, false, nil
	}
	item := fake.items[0]
	fake.items = fake.items[1:]
	return item, true, nil
}

func (fake *importExecutorFixture) Finish(_ context.Context, _ model.ExecutionIdentity, _ string, outcome model.ItemOutcome) error {
	fake.outcomes = append(fake.outcomes, outcome)
	return fake.record("finish")
}

func (fake *importExecutorFixture) Resume(context.Context, model.Work, model.ExecutionItem) (bool, error) {
	return fake.resumed, fake.record("resume")
}

func (fake *importExecutorFixture) Create(_ context.Context, _ model.Work, _ model.ExecutionItem, files []libraryimportmodel.ServerSourceFile) error {
	fake.reviewFiles = files
	return fake.record("review")
}

func (fake *importExecutorFixture) CopyFile(_ context.Context, _ model.Work, file model.ExecutionFile) (model.VerifiedBlob, error) {
	return model.VerifiedBlob{SHA256: file.Path, Size: file.Size}, fake.record("copy:" + file.Path)
}

func (fake *importExecutorFixture) CopyAsset(_ context.Context, _ model.Work, asset model.ExecutionAsset) (model.VerifiedBlob, bool, error) {
	return model.VerifiedBlob{SHA256: asset.Path, Size: asset.Size}, fake.validMedia, fake.record("media:" + asset.Path)
}

func (fake *importExecutorFixture) Copy(_ context.Context, _ model.ExecutionIdentity, source model.MaterialSource, _ model.VerifiedBlob) (string, error) {
	return "blob:" + source.Path, fake.record("bind:" + source.Path)
}

func (fake *importExecutorFixture) Warning(_ context.Context, _ model.ExecutionIdentity, source model.MaterialSource, code string) error {
	fake.warning = code
	return fake.record("warning:" + source.Path)
}

func (fake *importExecutorFixture) SetPhase(_ context.Context, _ model.ExecutionIdentity, phase string) error {
	return fake.record("phase:" + phase)
}

func (fake *importExecutorFixture) Cancelled(context.Context, model.ExecutionIdentity) (bool, error) {
	return fake.cancelled, fake.record("checkpoint")
}

func (fake *importExecutorFixture) Find(context.Context, model.ExecutionIdentity, string) ([]model.CompanionCandidate, error) {
	return fake.companions, fake.record("companions")
}

func (fake *importExecutorFixture) Record(_ context.Context, _ model.ExecutionIdentity, _ string, candidate model.CompanionCandidate, _ model.VerifiedBlob) (string, error) {
	return "companion:" + candidate.File.Path, fake.record("companion:" + candidate.File.Path)
}

func (*importExecutorFixture) Sanitize(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (*importExecutorFixture) DatabaseCause(error) string { return "" }

type executorCompletionFixture struct{ fake *importExecutorFixture }

func (fixture executorCompletionFixture) Finish(context.Context, model.ExecutionIdentity) error {
	return fixture.fake.record("complete")
}

type executorCancellationFixture struct{ fake *importExecutorFixture }

func (fixture executorCancellationFixture) Cancelled(context.Context, model.ExecutionIdentity) (bool, error) {
	return false, fixture.fake.record("cancel")
}

func (fixture executorCancellationFixture) Fail(context.Context, model.ExecutionIdentity, model.ExecutionFailure) error {
	return fixture.fake.record("fail")
}
