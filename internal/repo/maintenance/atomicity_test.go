package maintenance

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/model/maintenance"
	"retrom/internal/testkit/testsupport"
)

func TestRestoreSecurityFailureRollsBackRevocationsAndAudit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retrom.db")
	database, err := testsupport.OpenDatabase(t.Context(), path, func() time.Time { return time.UnixMilli(100) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile','Fixture',0);
 INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES('user','profile','fixture','Fixture','ADMIN','ENABLED',0,0);
 INSERT INTO auth_sessions(id,user_id,token_sha256,user_session_version,created_at_ms,last_seen_at_ms,
 idle_expires_at_ms,absolute_expires_at_ms) VALUES('session','user',zeroblob(32),1,0,0,1000,10000);
 INSERT INTO account_links(id,kind,invited_role,created_by_user_id,created_at_ms,expires_at_ms)
 VALUES('link','INVITATION','USER','user',0,3600000);`)
	if err != nil {
		t.Fatal(err)
	}
	err = New().WithRestore(t.Context(), path, func(records maintenance.RestoreRecords) error {
		counts, err := records.RevokeAccess(t.Context(), 100)
		if err != nil {
			return err
		}
		if counts.Sessions != 1 || counts.Links != 1 {
			t.Fatalf("revocation counts: %+v", counts)
		}
		if _, err := records.StopExternalImports(t.Context(), 100); err != nil {
			return err
		}
		if err := records.StopBulkApprovals(t.Context(), 100); err != nil {
			return err
		}
		if err := records.Audit(t.Context(), maintenance.FenceAudit{ID: "audit", Now: 100}); err != nil {
			return err
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("restore failure: %v", err)
	}
	var sessions, links, audits int
	err = database.SQL.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM auth_sessions WHERE revoked_at_ms IS NULL),
 (SELECT count(*) FROM account_links WHERE revoked_at_ms IS NULL AND version=1),
 (SELECT count(*) FROM audit_events WHERE id='audit')`).Scan(&sessions, &links, &audits)
	if err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || links != 1 || audits != 0 {
		t.Fatalf("partial security restore: %d %d %d", sessions, links, audits)
	}
}
