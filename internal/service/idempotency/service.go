package idempotency

import (
	"context"
	"errors"
	"fmt"
)

var ErrInvalidReceipt = errors.New("IDEMPOTENCY_RECEIPT_INVALID")

type Service struct {
	repository Repository
}

func New(repository Repository) *Service {
	return &Service{repository: repository}
}

func (service *Service) PurgeExpired(
	ctx context.Context, operationID, key, principalID string, nowMS int64,
) error {
	if err := service.repository.DeleteExpired(ctx, operationID, key, principalID, nowMS); err != nil {
		return fmt.Errorf("idempotency: purge expired receipt: %w", err)
	}
	return nil
}

func (service *Service) Lookup(
	ctx context.Context, operationID, key, principalID string,
) (Receipt, bool, error) {
	receipt, found, err := service.repository.Find(ctx, operationID, key, principalID)
	if err != nil {
		return Receipt{}, false, fmt.Errorf("idempotency: lookup receipt: %w", err)
	}
	return receipt, found, nil
}

func (service *Service) Store(
	ctx context.Context,
	operationID, key, principalID string,
	receipt Receipt,
	createdAtMS, expiresAtMS int64,
) error {
	if operationID == "" || key == "" || principalID == "" || receipt.RequestDigest == "" ||
		receipt.HTTPStatus < 100 || createdAtMS < 0 || expiresAtMS < createdAtMS {
		return ErrInvalidReceipt
	}
	if err := service.repository.Save(
		ctx, operationID, key, principalID, receipt, createdAtMS, expiresAtMS,
	); err != nil {
		return fmt.Errorf("idempotency: store receipt: %w", err)
	}
	return nil
}
