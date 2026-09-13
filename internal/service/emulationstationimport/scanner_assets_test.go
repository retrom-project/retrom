package emulationstationimport

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/adapter/files/mediaasset"
	"retrom/internal/capability/format/emulationstationmeta"
)

func TestScannerAssetFailuresKeepWarningPolicyExceptCancellation(t *testing.T) {
	for _, kind := range []string{"COVER", "VIDEO"} {
		for _, failure := range []struct {
			name  string
			cause error
			state string
		}{
			{name: "read", cause: ErrScanReadFailed, state: "READ_FAILED"},
			{name: "changed", cause: ErrSourceChanged, state: "SOURCE_CHANGED"},
			{name: "invalid", cause: errors.New("invalid media"), state: "INVALID"},
			{name: "cancel", cause: context.Canceled},
			{name: "too large", cause: mediaasset.ErrVideoTooLarge, state: "TOO_LARGE"},
		} {
			t.Run(kind+"/"+failure.name, func(t *testing.T) {
				memory := newScannerMemory()
				memory.assetErr = failure.cause
				scanner := NewScanner(memory)
				asset, warning, err := scanner.projectAsset(
					t.Context(),
					"gamelist.xml",
					kind,
					emulationstationmeta.AssetReference{RelativePath: "cover.png"},
					map[string]discoveredFile{"cover.png": {Path: "cover.png", Size: 1, Facts: "frozen"}},
				)
				if failure.name == "cancel" {
					if !errors.Is(err, context.Canceled) || asset != nil || warning != nil {
						t.Fatalf("asset=%#v warning=%v error=%v", asset, warning, err)
					}
					return
				}
				expected := failure.state
				if kind == "COVER" && failure.name == "too large" {
					expected = "INVALID"
				}
				if err != nil || asset == nil || asset.State != expected || warning["pathKind"] != kind {
					t.Fatalf("asset=%#v warning=%v error=%v", asset, warning, err)
				}
			})
		}
	}
}

func TestScannerPlaylistCancellationKeepsCause(t *testing.T) {
	memory := newScannerMemory()
	memory.readErr = context.Canceled
	scanner := NewScanner(memory)
	result, err := scanner.projectContent(
		t.Context(),
		"gamelist.xml",
		"game.m3u",
		"",
		map[string]discoveredFile{"game.m3u": {Path: "game.m3u", Size: 1, Facts: "frozen"}},
		&scanCaches{discCandidates: nil},
	)
	if !errors.Is(err, context.Canceled) || result.discoveryCode != "" {
		t.Fatalf("projection=%#v error=%v", result, err)
	}
}
