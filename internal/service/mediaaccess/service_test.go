package mediaaccess

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/mediaaccess"
)

func TestReviewAccessPreservesPrimaryPriorityAndRejectsAmbiguousSource(t *testing.T) {
	primary := model.ReviewAsset{Resource: model.Resource{Digest: "primary"}, Kind: "CANDIDATE", State: "READY", ItemState: "REVIEW_PENDING"}
	memory := &accessMemory{primary: []model.ReviewAsset{primary}}
	actual, err := New(memory).Review(t.Context(), "asset", "invalid-source-kind")
	if err != nil || actual.Digest != "primary" || memory.sourceReads != 0 {
		t.Fatalf("primary priority changed: %+v %v reads=%d", actual, err, memory.sourceReads)
	}
	memory.primary = nil
	memory.sources = []model.ReviewAsset{
		{Resource: model.Resource{Digest: "first"}, Kind: "PEGASUS", State: "COPIED", ItemState: "REVIEW_PENDING"},
		{Resource: model.Resource{Digest: "second"}, Kind: "EMULATIONSTATION", State: "COPIED", TerminalReview: true},
	}
	if _, err := New(memory).Review(t.Context(), "source", "COVER"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ambiguous source was published: %v", err)
	}
	memory.sources = memory.sources[:1]
	actual, err = New(memory).Review(t.Context(), "source", "")
	if err != nil || actual.Digest != "first" || memory.lastKind != "COVER" {
		t.Fatalf("default source selection changed: %+v %v kind=%s", actual, err, memory.lastKind)
	}
}

func TestMediaAccessPreservesCausesAndDoesNotFallbackAfterStorageFailure(t *testing.T) {
	cause := errors.New("snapshot unavailable")
	memory := &accessMemory{cause: cause}
	service := New(memory)
	for _, call := range []func() error{
		func() error { _, err := service.Game(t.Context(), "asset"); return err },
		func() error { _, err := service.Save(t.Context(), "save", "profile"); return err },
		func() error { _, err := service.Review(t.Context(), "asset", "COVER"); return err },
	} {
		if err := call(); !errors.Is(err, cause) || errors.Is(err, ErrNotFound) {
			t.Fatalf("storage cause changed: %v", err)
		}
	}
	if memory.sourceReads != 0 {
		t.Fatalf("storage failure incorrectly fell back %d times", memory.sourceReads)
	}
}

type accessMemory struct {
	game        model.GameAsset
	save        model.SaveScreenshot
	primary     []model.ReviewAsset
	sources     []model.ReviewAsset
	cause       error
	sourceReads int
	lastKind    string
}

func (memory *accessMemory) WithRead(_ context.Context, work func(model.Reader) error) error {
	return work(memory)
}

func (memory *accessMemory) Game(context.Context, string) (model.GameAsset, bool, error) {
	return memory.game, true, memory.cause
}

func (memory *accessMemory) Save(context.Context, string) (model.SaveScreenshot, bool, error) {
	return memory.save, true, memory.cause
}

func (memory *accessMemory) Review(context.Context, string) ([]model.ReviewAsset, error) {
	return memory.primary, memory.cause
}

func (memory *accessMemory) Sources(_ context.Context, _, kind string) ([]model.ReviewAsset, error) {
	memory.sourceReads++
	memory.lastKind = kind
	return memory.sources, memory.cause
}
