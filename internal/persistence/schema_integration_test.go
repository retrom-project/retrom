//go:build integration

package persistence_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/testsupport"
)

func TestSchemaAndSessionLifecycle(t *testing.T) {
	repository := testsupport.Database(t)
	assertEmptyApplicationSchema(t, repository)
	assertRevokedSessionLifecycle(t, repository)
}

func assertEmptyApplicationSchema(t *testing.T, repository *persistence.Repository) {
	t.Helper()
	ctx := t.Context()
	tableCount, err := repository.Count(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind='r' AND c.relname LIKE '%\_tab' ESCAPE '\'
 AND c.relname <> 'schema_migrations_tab'`)
	if err != nil || tableCount != 19 {
		t.Fatalf("tables=%d err=%v", tableCount, err)
	}
	unsuffixed, err := repository.Count(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind='r' AND c.relname NOT LIKE '%\_tab' ESCAPE '\'`)
	if err != nil || unsuffixed != 0 {
		t.Fatalf("unsuffixed tables=%d err=%v", unsuffixed, err)
	}
	ledger, err := repository.Count(ctx, "SELECT count(*) FROM schema_migrations_tab")
	if err != nil || ledger != 2 {
		t.Fatalf("applied migrations=%d err=%v", ledger, err)
	}
	prohibited,
		err := repository.Count(ctx,
		`SELECT count(*) FROM pg_constraint k JOIN pg_namespace n ON n.oid=k.connamespace
 WHERE n.nspname='public' AND k.contype IN ('c','f')`)
	if err != nil || prohibited != 0 {
		t.Fatalf("constraints=%d err=%v", prohibited, err)
	}
	views, err := repository.Count(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN ('v','m')`)
	if err != nil || views != 0 {
		t.Fatalf("views=%d err=%v", views, err)
	}
	directories, err := repository.Directories(ctx, true)
	if err != nil || len(directories) != 0 {
		t.Fatalf("directories=%v err=%v", directories, err)
	}
}

func assertRevokedSessionLifecycle(t *testing.T, repository *persistence.Repository) {
	t.Helper()
	ctx := t.Context()
	user := model.User{ID: uuid.NewString(), Username: "administrator", DisplayName: "Administrator", Role: "admin"}
	err := repository.Transaction(ctx, func(tx *persistence.Repository) error {
		if writeErr := tx.Initialize(ctx, user, "test-hash", 1000); writeErr != nil {
			return writeErr
		}
		return tx.CreateSession(ctx, uuid.NewString(), user.ID, "session-secret", 1000)
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := repository.Authenticate(ctx, "session-secret", 2000)
	if err != nil || principal.User.ID != user.ID {
		t.Fatalf("principal=%v err=%v", principal, err)
	}
	if err = repository.Logout(ctx, principal.SessionID, 3000); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Authenticate(ctx, "session-secret", 4000); !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("revoked err=%v", err)
	}
}

func TestVerifiedPasswordFenceAndShortLivedScanProgress(t *testing.T) {
	f := testsupport.Library(t)
	ctx := t.Context()
	if err := f.Repository.Transaction(ctx, func(tx *persistence.Repository) error {
		return tx.ChangePassword(ctx, f.Principal.User.ID, "new-hash", 2000)
	}); err != nil {
		t.Fatal(err)
	}
	err := f.Repository.Transaction(ctx, func(tx *persistence.Repository) error {
		if err := tx.FenceCredential(ctx, f.Principal.User.ID, "test-hash"); err != nil {
			return err
		}
		return tx.CreateSession(ctx, uuid.NewString(), f.Principal.User.ID, "old-password-token", 3000)
	})
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Fatalf("stale verification=%v", err)
	}
	if count, err := f.Repository.Count(ctx, "SELECT count(*) FROM auth_session_tab"); err != nil || count != 0 {
		t.Fatalf("sessions=%d error=%v", count, err)
	}
	for _, value := range []struct {
		status  string
		updated int64
	}{{"completed", 1000}, {"failed", 1000}, {"interrupted", 1000}, {"cancelled", 1000}, {"running", 1000}, {"completed", 10000}} {
		scan := model.Scan{ID: uuid.NewString(), ScanType: "game", Status: value.status, CreatedAtMs: 1000, UpdatedAtMs: value.updated}
		if err := f.Repository.CreateScan(ctx, scan, f.Principal.User.ID); err != nil {
			t.Fatal(err)
		}
		if err := f.Repository.UpdateScan(ctx, scan); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Repository.ExpireScanProgress(ctx, 5000); err != nil {
		t.Fatal(err)
	}
	if count, err := f.Repository.Count(ctx, "SELECT count(*) FROM scan_progress_tab"); err != nil || count != 2 {
		t.Fatalf("retained scans=%d error=%v", count, err)
	}
}
