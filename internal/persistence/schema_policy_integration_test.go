//go:build integration

package persistence_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"retrom/internal/persistence"
	"retrom/internal/testsupport"
	"retrom/migrations"
)

var schemaPolicyQueries = map[string]string{
	"schemas": `SELECT count(*) FROM pg_namespace WHERE nspname <> 'public'
 AND nspname <> 'information_schema' AND nspname NOT LIKE 'pg\_%' ESCAPE '\'`,
	"tables": `WITH expected(name) AS (VALUES
 ('instance_state_tab'),('user_tab'),('user_credential_tab'),('auth_session_tab'),('account_link_tab'),
 ('platform_instance_tab'),('platform_instance_core_tab'),('game_tab'),('game_file_tab'),
 ('game_file_member_tab'),('game_media_tab'),('save_tab'),('recent_game_tab'),('bios_file_tab'),
 ('scan_progress_tab'),('tag_tab'),('game_tag_tab'),('favorite_tab'),('favorite_folder_tab'),
 ('favorite_folder_game_tab'),('schema_migrations_tab')),
 actual(name) AS (SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind='r')
 SELECT count(*) FROM ((SELECT * FROM expected EXCEPT SELECT * FROM actual)
 UNION ALL (SELECT * FROM actual EXCEPT SELECT * FROM expected)) differences`,
	"table-kinds": `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN ('p','f')`,
	"constraints": `SELECT count(*) FROM pg_constraint k JOIN pg_namespace n ON n.oid=k.connamespace
 WHERE n.nspname='public' AND k.contype IN ('c','f')`,
	"partial-indexes": `SELECT count(*) FROM pg_index x JOIN pg_class c ON c.oid=x.indexrelid
 JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND x.indpred IS NOT NULL`,
	"views": `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN ('v','m')`,
	"triggers": `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
 JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND NOT t.tgisinternal`,
	"domains": `SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
 WHERE n.nspname='public' AND t.typtype='d'`,
	"routines": `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='public'`,
	"directory-seeds": `SELECT (SELECT count(*) FROM platform_instance_tab)
 + (SELECT count(*) FROM platform_instance_core_tab)`,
}

func schemaPolicyViolations(ctx context.Context, repository *persistence.Repository) ([]string, error) {
	var violations []string
	for rule, query := range schemaPolicyQueries {
		count, err := repository.Count(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("schema policy %s: %w", rule, err)
		}
		if count != 0 {
			violations = append(violations, rule)
		}
	}
	return violations, nil
}

func assertSchemaPolicy(t *testing.T, repository *persistence.Repository) {
	t.Helper()
	violations, err := schemaPolicyViolations(t.Context(), repository)
	if err != nil || len(violations) != 0 {
		t.Fatalf("schema policy violations=%v err=%v", violations, err)
	}
	ordered, err := migrations.All()
	if err != nil {
		t.Fatal(err)
	}
	count, err := repository.Count(t.Context(), "SELECT count(*) FROM schema_migrations_tab")
	if err != nil || count != int64(len(ordered)) {
		t.Fatalf("migration ledger count=%d expected=%d err=%v", count, len(ordered), err)
	}
	for _, migration := range ordered {
		count, err = repository.Count(t.Context(), "SELECT count(*) FROM schema_migrations_tab WHERE id=$1", migration.ID)
		if err != nil || count != 1 {
			t.Fatalf("missing migration %s count=%d err=%v", migration.ID, count, err)
		}
	}
}

func TestSchemaPolicyRejectsForbiddenObjects(t *testing.T) {
	cases := map[string]string{
		"schemas":         "CREATE SCHEMA extra; CREATE VIEW extra.hidden AS SELECT 1 AS value",
		"tables":          "ALTER TABLE tag_tab RENAME TO job_tab",
		"table-kinds":     "CREATE TABLE jobs_tab(id TEXT) PARTITION BY HASH(id)",
		"constraints":     "ALTER TABLE user_tab ADD CONSTRAINT forbidden CHECK (length(id)>0)",
		"partial-indexes": "CREATE INDEX forbidden ON user_tab(id) WHERE status='active'",
		"views":           "CREATE VIEW forbidden AS SELECT id FROM user_tab",
		"domains":         "CREATE DOMAIN forbidden AS TEXT",
		"routines":        "CREATE FUNCTION forbidden() RETURNS integer LANGUAGE SQL AS 'SELECT 1'",
		"triggers": `CREATE FUNCTION forbidden() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
 CREATE TRIGGER forbidden BEFORE INSERT ON user_tab FOR EACH ROW EXECUTE FUNCTION forbidden()`,
		"directory-seeds": "INSERT INTO platform_instance_core_tab VALUES ('orphan','core',0)",
	}
	for rule, statement := range cases {
		t.Run(rule, func(t *testing.T) {
			repository := testsupport.Database(t)
			if err := repository.Execute(t.Context(), statement); err != nil {
				t.Fatal(err)
			}
			violations, err := schemaPolicyViolations(t.Context(), repository)
			if err != nil || !strings.Contains(strings.Join(violations, ","), rule) {
				t.Fatalf("rule %s was not detected: violations=%v err=%v", rule, violations, err)
			}
		})
	}
}
