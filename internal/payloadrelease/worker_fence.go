package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/payloadrelease"
)

func (service *Service) fenceWork(ctx context.Context, transaction *sql.Tx, job claimedJob) error {
	if err := service.worker.CheckInScope(ctx, repository.BindWorker(transaction), job.Work); err != nil {
		return fmt.Errorf("fence release execution: %w", err)
	}
	return nil
}

func (service *Service) commitWork(ctx context.Context, transaction *sql.Tx, job claimedJob) error {
	if err := service.fenceWork(ctx, transaction, job); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit payload effect: %w", err)
	}
	return nil
}
