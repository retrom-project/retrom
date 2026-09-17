package launch

import (
	"context"
	"errors"
	model "retrom/internal/model/launch"
	"testing"
)

func TestPreviewCreatorRejectsMissingIdentityBeforeStorage(t *testing.T) {
	creator := NewPreviewCreator(nil, nil, model.PreviewEnvironment{})
	result, err := creator.Create(t.Context(), model.ReviewPreviewRequest{})
	if !errors.Is(err, model.ErrReviewPreviewUnavailable) || result.PreviewID != "" {
		t.Fatalf("invalid creation identity: id=%q error=%v", result.PreviewID, err)
	}
}

func TestPreviewCreatorFreezesReceiptOnlyAfterCommit(t *testing.T) {
	t.Parallel()
	creator, repository, _, request := previewFixture(t)
	created, err := creator.Create(t.Context(), request)
	if err != nil || created.PreviewID != previewTestID || created.PlayURL != "/admin/review-previews/"+previewTestID || repository.transactions != 1 || len(repository.writes) != 1 {
		t.Fatalf("creation: id=%q transactions=%d writes=%d error=%v", created.PreviewID, repository.transactions, len(repository.writes), err)
	}
	plan := repository.writes[0]
	if plan.Source.Title != "game.bin" || plan.BootstrapEnd != 301000 || plan.HardEnd != 7201000 || plan.Content.BlobID != "game" || len(plan.CredentialHash) != 32 {
		t.Fatalf("frozen content or lifetime incorrect: %+v", plan.Source)
	}
	cause := errors.New("commit failed")
	repository.commitErr = cause
	result, err := creator.Create(t.Context(), request)
	if !errors.Is(err, cause) || result != (model.ReviewPreviewCreated{}) {
		t.Fatalf("failed commit returned receipt: id=%q error=%v", result.PreviewID, err)
	}
}

func TestPreviewCreatorKeepsStorageAndCancellationCauses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		configure func(*previewTestRepository, error)
		writes    int
	}{
		{"initial replay", func(r *previewTestRepository, e error) { r.replayErr = e }, 0},
		{"snapshot", func(r *previewTestRepository, e error) { r.snapshotErr = e }, 0},
		{"final replay", func(r *previewTestRepository, e error) { r.finalReplayErr = e }, 0},
		{"current", func(r *previewTestRepository, e error) { r.currentErr = e }, 0},
		{"restore", func(r *previewTestRepository, e error) { r.restoreErr = e }, 0},
		{"write", func(r *previewTestRepository, e error) { r.writeErr = e }, 1},
		{"commit", func(r *previewTestRepository, e error) { r.commitErr = e }, 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			creator, repository, _, request := previewFixture(t)
			restoreID := "restore"
			request.RestoreFromPreviewID = &restoreID
			repository.restore = previewFixtureRestore(repository)
			test.configure(repository, context.Canceled)
			result, err := creator.Create(t.Context(), request)
			if !errors.Is(err, context.Canceled) || result != (model.ReviewPreviewCreated{}) || len(repository.writes) != test.writes {
				t.Fatalf("cause/receipt boundary: id=%q writes=%d error=%v", result.PreviewID, len(repository.writes), err)
			}
		})
	}
}

func TestPreviewCreatorRejectsPreparationBeforeTransaction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		configure func(*PreviewCreator, *previewTestRepository, *previewTestProvider)
		cause     error
	}{
		{"missing source", func(_ *PreviewCreator, r *previewTestRepository, _ *previewTestProvider) { r.missingSnapshot = true }, model.ErrReviewPreviewUnavailable},
		{"missing provider", func(_ *PreviewCreator, _ *previewTestRepository, p *previewTestProvider) { p.absent = true }, model.ErrReviewPreviewUnavailable},
		{"bundle drift", func(_ *PreviewCreator, r *previewTestRepository, _ *previewTestProvider) {
			r.snapshot.Source.BundleSHA256 = "new"
		}, model.ErrReviewPreviewUnavailable},
		{"game undeclared", func(_ *PreviewCreator, _ *previewTestRepository, p *previewTestProvider) { p.target.Inputs = nil }, model.ErrReviewPreviewUnavailable},
		{"threads", func(_ *PreviewCreator, _ *previewTestRepository, p *previewTestProvider) {
			p.target.Capabilities.RequiresThreads = true
		}, model.ErrBlocked},
		{"missing game", func(_ *PreviewCreator, r *previewTestRepository, _ *previewTestProvider) {
			r.snapshot.SourceFiles = nil
		}, model.ErrReviewPreviewUnavailable},
		{"identity error", func(c *PreviewCreator, _ *previewTestRepository, _ *previewTestProvider) {
			c.environment.NewID = func() (string, error) { return "", context.Canceled }
		}, context.Canceled},
		{"invalid identity", func(c *PreviewCreator, _ *previewTestRepository, _ *previewTestProvider) {
			c.environment.NewID = func() (string, error) { return "not-an-id", nil }
		}, model.ErrReviewPreviewUnavailable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			creator, repository, provider, request := previewFixture(t)
			test.configure(creator, repository, provider)
			result, err := creator.Create(t.Context(), request)
			if !errors.Is(err, test.cause) || result.PreviewID != "" || repository.transactions != 0 {
				t.Fatalf("invalid preparation entered transaction: id=%q transactions=%d error=%v", result.PreviewID, repository.transactions, err)
			}
		})
	}
}
