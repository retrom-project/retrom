package pegasusimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	repository "retrom/internal/repo/pegasusimport"
	pegasusimportservice "retrom/internal/service/pegasusimport"
	"retrom/internal/testkit/testsupport"
)

func TestRecoveryRollsBackSeededReviewAndParentOnLateFailure(t *testing.T) {
	t.Parallel()
	service, _, _ := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET leased_until_ms=5 WHERE id='work'`)
	before := readHandoffState(t, service)

	cause := errors.New("recovery commit rejected")
	faultDB := testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.TrimSpace(query) == "COMMIT" {
				return cause
			}
			return nil
		},
	})
	storage := repository.NewRecovery(faultDB)
	recovery := pegasusimportservice.NewRecovery(storage, nil, service.now)
	if err := recovery.Recover(t.Context()); !errors.Is(err, cause) {
		t.Fatalf("late failure lost cause: %v", err)
	}
	if after := readHandoffState(t, service); after != before {
		t.Fatalf("partial review recovery: before=%#v after=%#v", before, after)
	}
	var state string
	if err := service.database.QueryRowContext(t.Context(), `SELECT state FROM jobs WHERE id='work'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RUNNING" {
		t.Fatalf("failed recovery closed job: %s", state)
	}
}

func TestRecoveryRequeuesAfterPreservingCreatedReviewExactlyOnce(t *testing.T) {
	t.Parallel()
	service, _, _ := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `UPDATE jobs SET leased_until_ms=5 WHERE id='work'`)
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := readHandoffState(t, service)
	if before.State != "REVIEW_PENDING" || before.DraftVersion != 2 || before.Audits != 1 {
		t.Fatalf("lost review: %#v", before)
	}
	if err := service.recoverWork(t.Context()); err != nil {
		t.Fatal(err)
	}
	if after := readHandoffState(t, service); after != before {
		t.Fatalf("duplicate recovery: %#v", after)
	}
}
