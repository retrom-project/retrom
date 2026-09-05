package libraryimport

import (
	"database/sql"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCopyRPGMakerRuntimePacksFreezesDraftSelections(t *testing.T) {
	database := runtimePackSelectionTestDatabase(t)
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	run := approvalRun{
		ctx: t.Context(), transaction: transaction, platformID: "rpgmaker",
		variantID: "variant", draftID: "draft",
	}
	if err := run.copyRPGMakerRuntimePacks(); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	want := []frozenRuntimePackSelection{
		{"variant", 1, "First", "first", "definition-1", "installation-1"},
		{"variant", 2, "Second", "second", "definition-2", "installation-2"},
	}
	if observed := frozenRuntimePackSelections(t, database); !reflect.DeepEqual(observed, want) {
		t.Fatalf("frozen runtime pack selections = %#v, want %#v", observed, want)
	}
}

type frozenRuntimePackSelection struct {
	variantID                                                  string
	slot                                                       int64
	declaredName, normalizedName, definitionID, installationID string
}

func runtimePackSelectionTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(t.Context(), `
CREATE TABLE review_draft_runtime_pack_selections(
  review_draft_id TEXT,slot INTEGER,declared_name TEXT,normalized_declared_name TEXT,
  definition_id TEXT,installation_id TEXT
);
CREATE TABLE game_variant_runtime_packs(
  game_variant_id TEXT,slot INTEGER,declared_name TEXT,normalized_declared_name TEXT,
  definition_id TEXT,installation_id TEXT
);
INSERT INTO review_draft_runtime_pack_selections VALUES
  ('draft',2,'Second','second','definition-2','installation-2'),
  ('draft',1,'First','first','definition-1','installation-1');
`); err != nil {
		t.Fatal(err)
	}
	return database
}

func frozenRuntimePackSelections(t *testing.T, database *sql.DB) []frozenRuntimePackSelection {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `
SELECT game_variant_id,slot,declared_name,normalized_declared_name,definition_id,installation_id
FROM game_variant_runtime_packs ORDER BY slot
`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	result := make([]frozenRuntimePackSelection, 0)
	for rows.Next() {
		var selection frozenRuntimePackSelection
		if err := rows.Scan(
			&selection.variantID, &selection.slot, &selection.declaredName,
			&selection.normalizedName, &selection.definitionID, &selection.installationID,
		); err != nil {
			t.Fatal(err)
		}
		result = append(result, selection)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
