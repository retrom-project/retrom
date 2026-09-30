package platforminstance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	platformpersistence "retrom/internal/persistence/platforminstance"
	"retrom/internal/service/platforminstance"
)

type cancelCreationCommit struct {
	platforminstance.Repository
	cancel context.CancelFunc
}

func (repository cancelCreationCommit) WithWrite(ctx context.Context, work func(platforminstance.WriteScope) error) error {
	return repository.Repository.WithWrite(ctx, func(scope platforminstance.WriteScope) error {
		if err := work(scope); err != nil {
			return err
		}
		repository.cancel()
		return nil
	})
}

func TestDirectoryCreationCommitFailureDoesNotExposeSuccess(t *testing.T) {
	service, database := newService(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failing := platforminstance.New(cancelCreationCommit{platformpersistence.New(database), cancel}, fixedCreationTime)
	input := platforminstance.CreateInput{PlatformID: "gba", DefaultCoreID: "mgba", Name: "Commit Failure"}
	response, err := failing.CreateIdempotent(ctx, actor(), testUserID, "commit-key", strings.Repeat("a", 64), input, true)
	if !errors.Is(err, context.Canceled) || response.Status != 0 || len(response.Body) != 0 {
		t.Fatalf("commit failure exposed success: response=%v error=%v", response, err)
	}
	var directories, receipts, audits int
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT
  (SELECT count(*) FROM platform_instances), (SELECT count(*) FROM idempotency_records),
  (SELECT count(*) FROM audit_events WHERE action='PLATFORM_INSTANCE_CREATED')`).Scan(&directories, &receipts, &audits); err != nil {
		t.Fatal(err)
	}
	if directories != 0 || receipts != 0 || audits != 0 {
		t.Fatalf("commit left %d directories, %d receipts, %d audits", directories, receipts, audits)
	}
	retry, err := service.CreateIdempotent(t.Context(), actor(), testUserID, "commit-key", strings.Repeat("a", 64), input, true)
	if err != nil || retry.Status != 201 || retry.Replayed {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
}

func TestDirectoryCreationReceiptScopesPrincipalAndExpires(t *testing.T) {
	service, database := newService(t)
	input := platforminstance.CreateInput{PlatformID: "gba", DefaultCoreID: "mgba", Name: "Receipt Scope"}
	first, err := service.CreateIdempotent(t.Context(), actor(), testUserID, "scope-key", strings.Repeat("a", 64), input, true)
	if err != nil {
		t.Fatal(err)
	}
	// Receipt principal identifiers form independent namespaces; the actor remains a valid account.
	second, err := service.CreateIdempotent(t.Context(), actor(), "other-principal", "scope-key", strings.Repeat("a", 64), input, true)
	if err != nil || second.Replayed || string(second.Body) == string(first.Body) {
		t.Fatalf("principal scope=%v err=%v", second, err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE idempotency_records SET expires_at_ms=created_at_ms+1 WHERE principal_id=?`, testUserID); err != nil {
		t.Fatal(err)
	}
	service = platforminstance.New(platformpersistence.New(database), func() time.Time { return fixedCreationTime().Add(time.Second) })
	expired, err := service.CreateIdempotent(t.Context(), actor(), testUserID, "scope-key", strings.Repeat("b", 64), input, true)
	if err != nil || expired.Replayed || string(expired.Body) == string(first.Body) {
		t.Fatalf("expiry=%v err=%v", expired, err)
	}
	var receipts int
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT count(*) FROM idempotency_records WHERE key='scope-key'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 2 {
		t.Fatalf("receipt namespaces=%d", receipts)
	}
}

func fixedCreationTime() time.Time { return time.UnixMilli(1_786_000_000_000) }
