package payloadrelease

import (
	"context"
	"database/sql"
)

func (service *Service) releasePegasusItem(ctx context.Context, job claimedJob) error {
	return service.releaseSourceImportItem(ctx, job, pegasusSourceImportItem)
}

func (service *Service) releaseLinkedPegasusItem(
	ctx context.Context,
	transaction *sql.Tx,
	publicItemID string,
	now int64,
) error {
	return service.releaseLinkedSourceImportItems(ctx, transaction, publicItemID, now, pegasusSourceImportItem)
}

func (service *Service) releasePegasusPayload(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
) error {
	return service.releaseSourceImportItemPayload(ctx, transaction, itemID, now, pegasusSourceImportItem)
}
