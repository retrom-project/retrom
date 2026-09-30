//go:build integration

package libraryimport

import (
	"context"
	"testing"

	dbapi "retrom/internal/database"
	repository "retrom/internal/persistence/libraryimport"
	service "retrom/internal/service/libraryimport"
)

type publicationDATChange struct {
	service.ReviewApprovalRepository
	database dbapi.DB
	datID    string
	calls    int
}

func (records *publicationDATChange) WithApproval(ctx context.Context, work func(service.ReviewApprovalScope) error) error {
	if err := records.ReviewApprovalRepository.WithApproval(ctx, work); err != nil {
		return err
	}
	records.calls++
	active := 0
	if records.calls > 1 {
		active = 1
	}
	_, err := records.database.ExecContext(ctx, `UPDATE dat_versions SET is_active=? WHERE id=?`, active, records.datID)
	return err
}

func changeDATBetweenPublicationTransactions(t *testing.T, importer *Service, database dbapi.DB, itemID string) {
	t.Helper()
	runtime, err := repository.ReadReviewRuntime(t.Context(), database, itemID)
	if err != nil || runtime.DATID == nil {
		t.Fatalf("current publication DAT=%+v error=%v", runtime, err)
	}
	datID := *runtime.DATID
	records := &publicationDATChange{ReviewApprovalRepository: repository.NewReviewApprovals(database), database: database, datID: datID}
	importer.approvals = service.NewReviewApprovals(records, importer.tags, importer.now, importer.blobs)
	t.Cleanup(func() {
		if _, err := database.ExecContext(context.Background(), `UPDATE dat_versions SET is_active=1 WHERE id=?`, datID); err != nil {
			t.Error(err)
		}
	})
}
