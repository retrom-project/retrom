package payloadrelease

import (
	"context"
	"database/sql"
)

func (service *Service) releaseEmulationStationItem(ctx context.Context, job claimedJob) error {
	return service.releaseSourceImportItem(ctx, job, emulationStationSourceImportItem)
}

func (service *Service) releaseLinkedEmulationStationItem(
	ctx context.Context,
	transaction *sql.Tx,
	publicItemID string,
	now int64,
) error {
	return service.releaseLinkedSourceImportItems(
		ctx, transaction, publicItemID, now, emulationStationSourceImportItem,
	)
}

func (service *Service) releaseEmulationStationPayload(
	ctx context.Context,
	transaction *sql.Tx,
	itemID string,
	now int64,
) error {
	return service.releaseSourceImportItemPayload(
		ctx, transaction, itemID, now, emulationStationSourceImportItem,
	)
}
