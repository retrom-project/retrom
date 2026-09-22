package mediaaccess

import (
	"errors"
	"testing"
)

func TestMediaAccessChecksCurrentGameAndSaveOwner(t *testing.T) {
	memory := &accessMemory{
		game: GameAsset{Resource: Resource{Digest: "asset"}, GameState: "PUBLISHED"},
		save: SaveScreenshot{Resource: Resource{Digest: "screenshot"}, ProfileID: "owner", GameState: "PUBLISHED"},
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

func TestReviewMediaPreservesReadyAndTerminalVisibility(t *testing.T) {
	for _, test := range []struct {
		name  string
		asset ReviewAsset
		allow bool
	}{
		{"pending candidate", ReviewAsset{Kind: "CANDIDATE", State: "PENDING", ItemState: "REVIEW_PENDING"}, false},
		{"ready current candidate", ReviewAsset{Kind: "CANDIDATE", State: "READY", ItemState: "REVIEW_PENDING"}, true},
		{"game candidate", ReviewAsset{Kind: "CANDIDATE", State: "READY", GameState: "PUBLISHED"}, true},
		{"finished without retained payload", ReviewAsset{Kind: "UPLOAD", ItemState: "DISCARDED"}, false},
		{"terminal upload", ReviewAsset{Kind: "UPLOAD", ItemState: "DISCARDED", TerminalReview: true}, true},
		{"current screenshot", ReviewAsset{Kind: "SCREENSHOT", ItemState: "REVIEW_PENDING"}, true},
		{"terminal screenshot", ReviewAsset{Kind: "SCREENSHOT", ItemState: "PUBLISHED", TerminalReview: true}, true},
		{"copied source", ReviewAsset{Kind: "SOURCE", State: "COPIED", ItemState: "REVIEW_PENDING"}, true},
		{"pending source", ReviewAsset{Kind: "SOURCE", State: "PENDING", TerminalReview: true}, false},
		{"terminal source", ReviewAsset{Kind: "SOURCE", State: "COPIED", TerminalReview: true}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.asset.Resource = Resource{Digest: "media"}
			memory := &accessMemory{primary: []ReviewAsset{test.asset}}
			result, err := New(memory).Review(t.Context(), "asset", "COVER")
			if test.allow && (err != nil || result.Digest != "media") || !test.allow && !errors.Is(err, ErrNotFound) {
				t.Fatalf("visibility changed: result=%+v error=%v allow=%t", result, err, test.allow)
			}
		})
	}
}
