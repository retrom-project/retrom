package scans

import (
	"testing"

	"retrom/internal/model"
)

func TestOriginalRetentionRequiresKRKRContentRewrite(t *testing.T) {
	before := []model.ContentFile{{LogicalKey: "data.xp3", SizeBytes: 10, SHA256: "original", ID: "old", StorageKey: "old/path"}}
	for _, test := range []struct {
		name, platform string
		after          []model.ContentFile
		want           bool
	}{
		{"same content new storage", "kirikiri", []model.ContentFile{{LogicalKey: "data.xp3", SizeBytes: 10, SHA256: "original", ID: "new", StorageKey: "new/path"}}, false},
		{"rewritten KRKR bytes", "kirikiri", []model.ContentFile{{LogicalKey: "data.xp3", SizeBytes: 10, SHA256: "changed"}}, true},
		{"renamed KRKR content", "kirikiri", []model.ContentFile{{LogicalKey: "new.xp3", SizeBytes: 10, SHA256: "original"}}, true},
		{"resized KRKR content", "kirikiri", []model.ContentFile{{LogicalKey: "data.xp3", SizeBytes: 11, SHA256: "original"}}, true},
		{"removed KRKR file", "kirikiri", nil, true},
		{"RPG Maker repack", "rpgmaker", before, false},
		{"other normalization", "tyranoscript", []model.ContentFile{{LogicalKey: "index.html", SizeBytes: 1, SHA256: "changed"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := sourceContent{Files: before}
			if got := source.requiresOrigin(model.Directory{PlatformID: test.platform}, test.after); got != test.want {
				t.Fatalf("retention=%v expected=%v", got, test.want)
			}
		})
	}
}
