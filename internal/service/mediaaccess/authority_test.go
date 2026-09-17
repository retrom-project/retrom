package mediaaccess

import (
	"errors"
	"testing"

	model "retrom/internal/model/mediaaccess"
)

func TestMediaAccessChecksCurrentGameAndSaveOwner(t *testing.T) {
	memory := &accessMemory{
		game: model.GameAsset{Resource: model.Resource{Digest: "asset"}, GameState: "PUBLISHED"},
		save: model.SaveScreenshot{Resource: model.Resource{Digest: "screenshot"}, ProfileID: "owner", GameState: "PUBLISHED"},
	}
	service := New(memory)
	if result, err := service.Game(t.Context(), "asset"); err != nil || result.Digest != "asset" {
		t.Fatalf("published asset denied: %+v %v", result, err)
	}
	memory.game.GameState = "DELETED"
	if _, err := service.Game(t.Context(), "asset"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted game's media exposed: %v", err)
	}
	if result, err := service.Save(t.Context(), "save", "owner"); err != nil || result.Digest != "screenshot" {
		t.Fatalf("own screenshot denied: %+v %v", result, err)
	}
	for _, invalid := range []struct {
		profile string
		deleted bool
		game    string
	}{{"other", false, "PUBLISHED"}, {"owner", true, "PUBLISHED"}, {"owner", false, "DELETED"}} {
		memory.save.Deleted, memory.save.GameState = invalid.deleted, invalid.game
		if _, err := service.Save(t.Context(), "save", invalid.profile); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unavailable screenshot exposed for %+v: %v", invalid, err)
		}
	}
}

func TestReviewMediaPreservesReadyAndHistoricalVisibility(t *testing.T) {
	for _, test := range []struct {
		name  string
		asset model.ReviewAsset
		allow bool
	}{
		{"pending candidate", model.ReviewAsset{Kind: "CANDIDATE", State: "PENDING", ItemState: "REVIEW_PENDING"}, false},
		{"ready current candidate", model.ReviewAsset{Kind: "CANDIDATE", State: "READY", ItemState: "REVIEW_PENDING"}, true},
		{"game candidate", model.ReviewAsset{Kind: "CANDIDATE", State: "READY", GameState: "PUBLISHED"}, true},
		{"finished without audit", model.ReviewAsset{Kind: "UPLOAD", ItemState: "DISCARDED"}, false},
		{"historical upload", model.ReviewAsset{Kind: "UPLOAD", ItemState: "DISCARDED", TerminalReview: true}, true},
		{"current screenshot", model.ReviewAsset{Kind: "SCREENSHOT", ItemState: "REVIEW_PENDING"}, true},
		{"historical screenshot", model.ReviewAsset{Kind: "SCREENSHOT", ItemState: "PUBLISHED", TerminalReview: true}, true},
		{"copied source", model.ReviewAsset{Kind: "PEGASUS", State: "COPIED", ItemState: "REVIEW_PENDING"}, true},
		{"pending source", model.ReviewAsset{Kind: "EMULATIONSTATION", State: "PENDING", TerminalReview: true}, false},
		{"historical source", model.ReviewAsset{Kind: "EMULATIONSTATION", State: "COPIED", TerminalReview: true}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.asset.Resource = model.Resource{Digest: "media"}
			memory := &accessMemory{primary: []model.ReviewAsset{test.asset}}
			result, err := New(memory).Review(t.Context(), "asset", "COVER")
			if test.allow && (err != nil || result.Digest != "media") || !test.allow && !errors.Is(err, ErrNotFound) {
				t.Fatalf("visibility changed: result=%+v error=%v allow=%t", result, err, test.allow)
			}
		})
	}
}
