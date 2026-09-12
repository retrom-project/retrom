package pegasusimport

import (
	"context"

	library "retrom/internal/service/libraryimport"
)

type importExecutorFixture struct {
	items       []ExecutionItem
	events      []string
	failures    map[string]error
	outcomes    []ItemOutcome
	reviewFiles []library.ServerSourceFile
	companions  []CompanionCandidate
	claims      int
	resumed     bool
	validMedia  bool
	cancelled   bool
	warning     string
}

func newImportExecutorFixture() (*importExecutorFixture, *ImportExecutor) {
	fake := &importExecutorFixture{
		items:    []ExecutionItem{{ID: "item", Files: []ExecutionFile{{Path: "game.gba", Size: 4}}}},
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

func (fake *importExecutorFixture) Next(context.Context, ExecutionIdentity) (ExecutionItem, bool, error) {
	fake.claims++
	if err := fake.record("next"); err != nil {
		return ExecutionItem{}, false, err
	}
	if len(fake.items) == 0 {
		return ExecutionItem{}, false, nil
	}
	item := fake.items[0]
	fake.items = fake.items[1:]
	return item, true, nil
}

func (fake *importExecutorFixture) Finish(_ context.Context, _ ExecutionIdentity, _ string, outcome ItemOutcome) error {
	fake.outcomes = append(fake.outcomes, outcome)
	return fake.record("finish")
}

func (fake *importExecutorFixture) Resume(context.Context, Work, ExecutionItem) (bool, error) {
	return fake.resumed, fake.record("resume")
}

func (fake *importExecutorFixture) Create(_ context.Context, _ Work, _ ExecutionItem, files []library.ServerSourceFile) error {
	fake.reviewFiles = files
	return fake.record("review")
}

func (fake *importExecutorFixture) CopyFile(_ context.Context, _ Work, file ExecutionFile) (VerifiedBlob, error) {
	return VerifiedBlob{SHA256: file.Path, Size: file.Size}, fake.record("copy:" + file.Path)
}

func (fake *importExecutorFixture) CopyAsset(_ context.Context, _ Work, asset ExecutionAsset) (VerifiedBlob, bool, error) {
	return VerifiedBlob{SHA256: asset.Path, Size: asset.Size}, fake.validMedia, fake.record("media:" + asset.Path)
}

func (fake *importExecutorFixture) Copy(_ context.Context, _ ExecutionIdentity, source MaterialSource, _ VerifiedBlob) (string, error) {
	return "blob:" + source.Path, fake.record("bind:" + source.Path)
}

func (fake *importExecutorFixture) Warning(_ context.Context, _ ExecutionIdentity, source MaterialSource, code string) error {
	fake.warning = code
	return fake.record("warning:" + source.Path)
}

func (fake *importExecutorFixture) SetPhase(_ context.Context, _ ExecutionIdentity, phase string) error {
	return fake.record("phase:" + phase)
}

func (fake *importExecutorFixture) Cancelled(context.Context, ExecutionIdentity) (bool, error) {
	return fake.cancelled, fake.record("checkpoint")
}

func (fake *importExecutorFixture) Find(context.Context, ExecutionIdentity, string) ([]CompanionCandidate, error) {
	return fake.companions, fake.record("companions")
}

func (fake *importExecutorFixture) Record(_ context.Context, _ ExecutionIdentity, _ string, candidate CompanionCandidate, _ VerifiedBlob) (string, error) {
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

func (fixture executorCompletionFixture) Finish(context.Context, ExecutionIdentity) error {
	return fixture.fake.record("complete")
}

type executorCancellationFixture struct{ fake *importExecutorFixture }

func (fixture executorCancellationFixture) Cancelled(context.Context, ExecutionIdentity) (bool, error) {
	return false, fixture.fake.record("cancel")
}

func (fixture executorCancellationFixture) Fail(context.Context, ExecutionIdentity, ExecutionFailure) error {
	return fixture.fake.record("fail")
}
