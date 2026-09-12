package contentprofile

import (
	"errors"
	"testing"

	"retrom/internal/importing"
)

func TestPokemonMiniSingleROMAdmission(t *testing.T) {
	for _, name := range []string{"game.min", "game.MIN"} {
		if !AcceptsRaw("pokemini", name) {
			t.Errorf("rejected %s", name)
		}
	}
	for _, name := range []string{"game.gb", "game.gba", "game.zip"} {
		if AcceptsRaw("pokemini", name) {
			t.Errorf("accepted %s", name)
		}
	}
	if !AcceptsArchive("pokemini", ArchiveZIP) {
		t.Fatal("single-disk archive rejected")
	}
	_, err := SelectArchivePrimary("pokemini", []importing.ArchiveEntry{{NormalizedPath: "game1.min"}, {NormalizedPath: "game2.min"}})
	if !errors.Is(err, ErrAmbiguousPrimaryContent) {
		t.Fatalf("multiple disks: %v", err)
	}
}
