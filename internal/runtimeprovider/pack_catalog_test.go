package runtimeprovider

import (
	"strings"
	"testing"
	"time"

	"retrom/internal/runtimecatalog"
)

func TestNewDeclaredPackUsesExistingLayoutWithoutSchemaChange(t *testing.T) {
	database := openProjectionDatabase(t)
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	if err := Reconcile(t.Context(), database.SQL, initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT group_concat(sql,';') FROM sqlite_schema WHERE sql IS NOT NULL`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	candidate := projectionFixture("1.0.0", "a", []string{"state-v1"})
	candidate.definitions.AssetPacks = []runtimecatalog.AssetPackDefinition{{
		ID: "additional-rtp", Kind: "RPG2003_RTP", Generation: "RPG2003", DeclaredName: "Extra Assets",
		NormalizedDeclaredName: "extra assets", DisplayName: "Additional assets", RequiredLayoutVersion: "easy-rtp-layout-v1", Enabled: true,
	}}
	candidate.catalogSHA256 = strings.Repeat("c", 64)
	if err := Reconcile(t.Context(), database.SQL, candidate, time.UnixMilli(2)); err != nil {
		t.Fatal(err)
	}
	if err := Reconcile(t.Context(), database.SQL, candidate, time.UnixMilli(3)); err != nil {
		t.Fatal(err)
	}
	var layout, origin, after string
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT required_layout_version,origin FROM runtime_asset_pack_definitions WHERE id='additional-rtp'`).Scan(&layout, &origin); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT group_concat(sql,';') FROM sqlite_schema WHERE sql IS NOT NULL`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before || layout != "easy-rtp-layout-v1" || origin != "BUILTIN" {
		t.Fatalf("schema or declaration drift: layout=%s origin=%s", layout, origin)
	}
}
