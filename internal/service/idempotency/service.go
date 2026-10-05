package idempotency

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrInvalidReceipt = errors.New("IDEMPOTENCY_RECEIPT_INVALID")
	ErrKeyReused      = errors.New("IDEMPOTENCY_KEY_REUSED")
)

type Service struct {
	repository Repository
	gates      gates
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
