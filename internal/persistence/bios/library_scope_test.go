package bios

import (
	"strings"
	"testing"

	"retrom/internal/database/postgres"
	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/bios"
)

func TestLibraryScopeSearchesProviderTargetAndCurrentPublishedGames(t *testing.T) {
	db, err := postgres.Open(testpostgres.DSN(t), postgres.Options{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(t.Context(), `CREATE TABLE games(id TEXT PRIMARY KEY,status TEXT,created_at_ms BIGINT);
CREATE INDEX games_latest ON games(status,created_at_ms DESC,id DESC);
CREATE TABLE game_variants(game_id TEXT,provider_id TEXT,target_id TEXT);
CREATE INDEX game_variants_game ON game_variants(game_id);
CREATE TABLE bios_requirements(id TEXT,provider_id TEXT,target_id TEXT,enabled BIGINT);
INSERT INTO games VALUES('visible','PUBLISHED',1),('deleted','DELETED',2);
INSERT INTO game_variants VALUES('visible','provider','used'),('deleted','provider','retired');
INSERT INTO bios_requirements VALUES('required','provider','used',1),('optional','provider','used',1),
('retired','provider','retired',1),('unused','provider','unused',1),('disabled','provider','used',0);`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "CREATE INDEX game_variants_provider_target_game ON game_variants(provider_id,target_id,game_id)"); err != nil {
		t.Fatal(err)
	}
	query := "SELECT count(*) FROM bios_requirements requirement WHERE enabled=1 AND " +
		scopeSQL(application.ScopeRequiredByLibrary)
	var count int
	if err := dbapi.QueryRowContext(t.Context(), db, query).Scan(&count); err != nil || count != 2 {
		t.Fatalf("library membership count=%d err=%v", count, err)
	}
	if _, err := db.ExecContext(t.Context(), "SET enable_seqscan=off"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(t.Context(), "EXPLAIN "+query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "game_variants_provider_target_game") {
		t.Fatalf("BIOS scope must search the Provider/Target index: %s", plan.String())
	}
}
