//go:build integration

package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"retrom/internal/dbexec"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type approvalMutatingReader struct {
	application.ReviewApprovalReader
	transaction *sql.Tx
	mutation    string
	importID    string
}

func (reader approvalMutatingReader) Head(ctx context.Context, itemID string) (application.ReviewApprovalHead, bool, error) {
	head, found, err := reader.ReviewApprovalReader.Head(ctx, itemID)
	if err != nil || !found {
		return head, found, err
	}
	_, err = reader.transaction.ExecContext(ctx, reader.mutation, reader.importID)
	return head, found, err
}

func TestApprovalFinalFenceRollsBackEarlierPublication(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, query string }{
		{"draft version", `UPDATE review_drafts SET version=version+1 WHERE import_item_id IN(SELECT id FROM import_items WHERE import_job_id=?)`},
		{"parent version", `UPDATE import_jobs SET version=version+1 WHERE id=?`},
		{"pending count", `UPDATE import_jobs SET review_pending_item_count=1,discarded_item_count=1 WHERE id=?`},
	} {
		t.Run(test.name, func(t *testing.T) { t.Parallel(); verifyApprovalFence(t, test.name, test.query) })
	}
}

func verifyApprovalFence(t *testing.T, stage, mutation string) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	created := fixture.create(t, "Approval final fence", "Retrom owned approval CAS", 2)
	before := approvalDatabaseRows(t, fixture.database)
	transaction, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(transaction)
	scope := repository.BindReviewApproval(transaction)
	scope.Reader = approvalMutatingReader{ReviewApprovalReader: scope.Reader, transaction: transaction, mutation: mutation, importID: created.Created.ImportJobID}
	result, err := fixture.service.reviewApprovals().ApproveInScope(t.Context(), scope, application.ReviewApprovalRequest{ItemID: created.Items[0].ItemID, ExpectedVersion: 1})
	if !errors.Is(err, ErrInvalid) || result != (Approved{}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var games, variants, published int
	if err := transaction.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM games),(SELECT count(*) FROM game_variants),(SELECT count(*) FROM import_items WHERE state='PUBLISHED')`).Scan(&games, &variants, &published); err != nil {
		t.Fatal(err)
	}
	expectedPublished := 1
	if stage == "draft version" {
		expectedPublished = 0
	}
	if games != 1 || variants != 1 || published != expectedPublished {
		t.Fatalf("fence did not follow real publication: games=%d variants=%d published=%d", games, variants, published)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertApprovalRowsUnchanged(t, fixture.database, before)
}

func TestApprovalRequiresCompletedSourceHandoff(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	before := approvalDatabaseRows(t, fixture.database)
	result, err := fixture.service.Approve(t.Context(), created.Items[0].ItemID, 1)
	if !errors.Is(err, ErrInvalid) || result != (Approved{}) {
		t.Fatalf("unhanded source result=%+v err=%v", result, err)
	}
	assertApprovalRowsUnchanged(t, fixture.database, before)
}
