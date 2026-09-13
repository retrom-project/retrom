package launch

import (
	"errors"
	"strings"
	"testing"

	"retrom/internal/capability/content/contentprofile"
)

func TestProjectContentIdentityIsOrderIndependentAndBindsProjection(t *testing.T) {
	t.Parallel()
	first := ConfigFile{
		LogicalName: "Data/System.json", Format: string(
			contentprofile.ContentKindRPGMakerProject,
		), Digest: strings.Repeat(
			"a",
			64,
		),
	}
	second := ConfigFile{
		LogicalName: "__retrom__/game.mkxpz", Format: string(
			contentprofile.ContentKindRPGMakerProject,
		), Digest: strings.Repeat(
			"b",
			64,
		),
	}
	identity, err := ProjectIdentity([]ConfigFile{first, second})
	reordered, reorderedErr := ProjectIdentity([]ConfigFile{second, first})
	changed, changedErr := ProjectIdentity([]ConfigFile{
		first,
		{LogicalName: second.LogicalName, Format: second.Format, Digest: strings.Repeat("c", 64)},
	})
	root, rootErr := RuntimeProjectContentRoot(identity)
	if err != nil || reorderedErr != nil || changedErr != nil || rootErr != nil ||
		identity != reordered || identity == changed ||
		root != RuntimeProjectContentPrefix+identity+"/" {
		t.Fatalf(
			"identity=%q reordered=%q changed=%q root=%q errors=%v/%v/%v/%v",
			identity,
			reordered,
			changed,
			root,
			err,
			reorderedErr,
			changedErr,
			rootErr,
		)
	}
}

func TestProjectContentIdentityRejectsAmbiguousFiles(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	tests := [][]ConfigFile{
		nil,
		{{LogicalName: "../escape", Format: string(contentprofile.ContentKindONSProject), Digest: digest}},
		{{LogicalName: "0.txt", Format: "SOURCE_V1", Digest: digest}},
		{{LogicalName: "0.txt", Format: string(contentprofile.ContentKindONSProject), Digest: "bad"}},
		{
			{LogicalName: "0.txt", Format: string(contentprofile.ContentKindONSProject), Digest: digest},
			{LogicalName: "0.txt", Format: string(contentprofile.ContentKindONSProject), Digest: strings.Repeat("b", 64)},
		},
		{
			{LogicalName: "0.txt", Format: string(contentprofile.ContentKindONSProject), Digest: digest},
			{LogicalName: "0.TXT", Format: string(contentprofile.ContentKindONSProject), Digest: strings.Repeat("b", 64)},
		},
		{
			{LogicalName: "0.txt", Format: string(contentprofile.ContentKindONSProject), Digest: digest},
			{
				LogicalName: "default.ttf",
				Format:      string(contentprofile.ContentKindKiriKiriProject),
				Digest:      strings.Repeat("b", 64),
			},
		},
	}
	for _, files := range tests {
		if _, err := ProjectIdentity(files); !errors.Is(err, ErrBlocked) {
			t.Fatalf("derive identity error=%v for %#v", err, files)
		}
	}
}
