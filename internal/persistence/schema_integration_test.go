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
	assertSchemaPolicy(t, repository)
	directories, err := repository.Directories(t.Context(), true)
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
