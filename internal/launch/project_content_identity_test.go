package launch

import (
	"errors"
	"strings"
	"testing"
)

func TestProjectContentIdentityIsOrderIndependentAndBindsProjection(t *testing.T) {
	t.Parallel()
	first := projectIdentityFile{
		logicalName: "Data/System.json", format: rpgProjectFormat, digest: strings.Repeat("a", 64),
	}
	second := projectIdentityFile{
		logicalName: "__retrom__/game.mkxpz", format: rpgProjectFormat, digest: strings.Repeat("b", 64),
	}
	identity, err := deriveProjectContentIdentity([]projectIdentityFile{first, second})
	reordered, reorderedErr := deriveProjectContentIdentity([]projectIdentityFile{second, first})
	changed, changedErr := deriveProjectContentIdentity([]projectIdentityFile{
		first,
		{logicalName: second.logicalName, format: second.format, digest: strings.Repeat("c", 64)},
	})
	root, rootErr := RuntimeProjectContentRoot(identity)
	if err != nil || reorderedErr != nil || changedErr != nil || rootErr != nil ||
		identity != reordered || identity == changed ||
		root != RuntimeProjectContentPrefix+identity+"/" {
		t.Fatalf("identity=%q reordered=%q changed=%q root=%q errors=%v/%v/%v/%v", identity, reordered, changed, root, err, reorderedErr, changedErr, rootErr)
	}
}

func TestProjectContentIdentityRejectsAmbiguousFiles(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	tests := [][]projectIdentityFile{
		nil,
		{{logicalName: "../escape", format: onsProjectFormat, digest: digest}},
		{{logicalName: "0.txt", format: "SOURCE_V1", digest: digest}},
		{{logicalName: "0.txt", format: onsProjectFormat, digest: "bad"}},
		{
			{logicalName: "0.txt", format: onsProjectFormat, digest: digest},
			{logicalName: "0.txt", format: onsProjectFormat, digest: strings.Repeat("b", 64)},
		},
		{
			{logicalName: "0.txt", format: onsProjectFormat, digest: digest},
			{logicalName: "0.TXT", format: onsProjectFormat, digest: strings.Repeat("b", 64)},
		},
		{
			{logicalName: "0.txt", format: onsProjectFormat, digest: digest},
			{logicalName: "default.ttf", format: kirikiriProjectFormat, digest: strings.Repeat("b", 64)},
		},
	}
	for _, files := range tests {
		if _, err := deriveProjectContentIdentity(files); !errors.Is(err, ErrBlocked) {
			t.Fatalf("derive identity error=%v for %#v", err, files)
		}
	}
}
