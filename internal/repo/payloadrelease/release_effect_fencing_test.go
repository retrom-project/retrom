package payloadrelease

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/payloadrelease"
	"retrom/internal/testkit/testsupport"

	"modernc.org/sqlite"
)

func effectRepositoryDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "effect.db"), func() time.Time { return time.UnixMilli(10) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = db.SQL.ExecContext(t.Context(), `INSERT INTO games(id,platform_instance_id,title,title_initial,
 description,developer,publisher,genre,metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,
 source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms)
 VALUES('effect-game',(SELECT id FROM platform_instances WHERE catalog_template_key='gba/mgba'),
 'Effect','E','','','','','ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE','effect-source','{}',?,
 'PUBLISHED','effect',1,1,1)`, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	if _, err := application.NewScheduler(nil).DeleteGame(t.Context(), BindScheduling(tx), "effect-game", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db.SQL
}

func TestEffectOwnerCASFencesLateVersionAndSourceDrift(t *testing.T) {
	for _, mutation := range []string{"version=version+1", "content_source_ref_id='replacement'", "metadata_source_kind='IMPORT_REVIEW',metadata_source_ref_id='replacement'"} {
		t.Run(mutation, func(t *testing.T) {
			db := effectRepositoryDatabase(t)
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer dbexec.Rollback(tx)
			scope := BindEffects(tx)
			before, err := scope.Read.Owner(t.Context(), application.Scope{Type: application.ScopeGame, ID: "effect-game"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), "UPDATE games SET "+mutation+" WHERE id='effect-game'"); err != nil {
				t.Fatal(err)
			}
			after := before.Owner
			after.PayloadState = "RELEASED"
			after.Version++
			err = scope.Write.ChangeOwner(t.Context(), application.EffectOwnerChange{Before: before, After: after, Released: true, NowMS: 10})
			if !errors.Is(err, application.ErrEffectConflict) {
				t.Fatalf("late drift error=%v", err)
			}
		})
	}
}

func TestEffectCommitFailurePreservesCauseAndRollsBackOwner(t *testing.T) {
	db := effectRepositoryDatabase(t)
	err := NewReleaseEffects(db).WithEffects(t.Context(), func(scope application.EffectScope) error {
		before, err := scope.Read.Owner(t.Context(), application.Scope{Type: application.ScopeGame, ID: "effect-game"})
		if err != nil {
			return err
		}
		after := before.Owner
		after.PayloadState = "RELEASED"
		after.Version++
		if err := scope.Write.ChangeOwner(t.Context(), application.EffectOwnerChange{Before: before, After: after, Released: true, NowMS: 10}); err != nil {
			return err
		}
		records, ok := scope.Read.(effectRecords)
		if !ok {
			t.Fatal("real effect records required")
		}
		if _, err := records.executor.ExecContext(t.Context(), `PRAGMA defer_foreign_keys=ON`); err != nil {
			return err
		}
		_, err = records.executor.ExecContext(t.Context(), `INSERT INTO game_assets(id,game_id,blob_id,kind,ordinal,width_px,height_px,media_type,created_at_ms)
VALUES('deferred-effect','effect-game','missing-deferred-blob','COVER',0,1,1,'image/png',10)`)
		return err
	})
	var cause *sqlite.Error
	if !errors.As(err, &cause) || cause.Code() != 787 {
		t.Fatalf("deferred commit cause=%v", err)
	}
	var state string
	var assets int
	if err := db.QueryRowContext(t.Context(), `SELECT payload_state,(SELECT count(*) FROM game_assets WHERE id='deferred-effect') FROM games WHERE id='effect-game'`).Scan(&state, &assets); err != nil || state != "RELEASING" || assets != 0 {
		t.Fatalf("partial commit state=%s assets=%d error=%v", state, assets, err)
	}
}
