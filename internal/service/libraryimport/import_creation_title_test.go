package libraryimport

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestImportDraftRequiresUsableSourceTitle(t *testing.T) {
	for _, test := range []struct {
		name, source, logical, want string
		explicit                    bool
	}{
		{name: "archive", source: "projects/Mystic Sunrise.zip", want: "Mystic Sunrise", explicit: true},
		{name: "directory", source: "Mystic Sunrise", want: "Mystic Sunrise", explicit: true},
		{name: "ordinary file", logical: "games/Golden Sun.gba", want: "Golden Sun"},
		{name: "nameless project", logical: "CharSet/hero.png", explicit: true},
		{name: "blank name", source: " .zip", explicit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := creationCommit{}
			group := creationGroup{group: PreparedGroup{TitleSource: test.source, TitleSourceExplicit: test.explicit, Sources: []PreparedSource{{LogicalName: test.logical}}}}
			draft, err := run.draftChange(&group)
			if test.want == "" {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("nameless source produced a review: %+v %v", draft, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var metadata struct {
				Title string `json:"title"`
			}
			if err = json.Unmarshal([]byte(draft.MetadataJSON), &metadata); err != nil || metadata.Title != test.want {
				t.Fatalf("title = %q: %v", metadata.Title, err)
			}
		})
	}
}
