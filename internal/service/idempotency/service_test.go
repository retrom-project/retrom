package idempotency

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/idempotency"
)

type memoryRepository struct {
	receipt model.Receipt
	found   bool
	stored  bool
}

func (repository *memoryRepository) DeleteExpired(context.Context, string, string, string, int64) error {
	return nil
}

func (repository *memoryRepository) Find(context.Context, string, string, string) (model.Receipt, bool, error) {
	return repository.receipt, repository.found, nil
}

func (repository *memoryRepository) Save(
	_ context.Context, _ string, _ string, _ string, receipt model.Receipt, _, _ int64,
) error {
	repository.receipt = receipt
	repository.stored = true
	return nil
}

func TestStoreAndLookupReceipt(t *testing.T) {
	repository := &memoryRepository{}
	service := New(repository)
	want := model.Receipt{RequestDigest: "digest", HTTPStatus: 201, HeadersJSON: "{}", Body: []byte("{}")}
	if err := service.Store(t.Context(), "operation", "key", "principal", want, 10, 20); err != nil {
		t.Fatal(err)
	}
	if !repository.stored || repository.receipt.RequestDigest != want.RequestDigest {
		t.Fatalf("stored receipt = %#v", repository.receipt)
	}
	repository.found = true
	got, found, err := service.Lookup(t.Context(), "operation", "key", "principal")
	if err != nil || !found || string(got.Body) != "{}" {
		t.Fatalf("lookup = %#v, found=%v, err=%v", got, found, err)
	}
}

func TestStoreRejectsInvalidReceipt(t *testing.T) {
	service := New(&memoryRepository{})
	if err := service.Store(t.Context(), "operation", "key", "principal", model.Receipt{}, 10, 20); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatal("invalid receipt accepted")
	}
	if err := service.Store(t.Context(), "", "key", "principal", model.Receipt{RequestDigest: "d", HTTPStatus: 200}, 10, 20); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatal("missing operation accepted")
	}
}
