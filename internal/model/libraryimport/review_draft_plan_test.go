package libraryimport

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMergeMetadataPreservesExplicitNullAndOmittedFields(t *testing.T) {
	t.Parallel()
	var patch MetadataPatch
	if err := json.Unmarshal([]byte(`{"players":null,"releaseYear":2025,"title":"New title"}`), &patch); err != nil {
		t.Fatal(err)
	}
	current := map[string]any{"title": "Old title", "developer": "Developer"}
	metadata, err := MergeMetadata(current, &patch, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if metadata["title"] != "New title" || metadata["developer"] != "Developer" {
		t.Fatalf("metadata = %#v", metadata)
	}
	if value, exists := metadata["players"]; !exists || value != nil {
		t.Fatalf("players = %#v, want explicit null", value)
	}
	if metadata["releaseYear"] != int64(2025) {
		t.Fatalf("releaseYear = %#v", metadata["releaseYear"])
	}
}

func TestSearchTextUsesThePurePlanParts(t *testing.T) {
	t.Parallel()
	parts := SearchParts("item", []string{"Roms/Game.zip"}, map[string]any{"title": "My Game"})
	if got := SearchText(parts); got != "item roms/game.zip my game" {
		t.Fatalf("search text = %q", got)
	}
}
