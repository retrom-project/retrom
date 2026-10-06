package libraryimport

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"retrom/internal/filestore"
)

type blockedPublicationRepository struct {
	ReviewApprovalRepository
	PublicationRecords
	blocked chan struct{}
	calls   map[string]int
}

func (repository *blockedPublicationRepository) WithApproval(_ context.Context, work func(ReviewApprovalScope) error) error {
	return work(ReviewApprovalScope{Publications: repository})
}

func (repository *blockedPublicationRepository) ReadPublication(ctx context.Context, id string) (PublicationState, error) {
	repository.calls[id]++
	if id == "slow" {
		select {
		case <-repository.blocked:
		case <-ctx.Done():
			return PublicationState{}, ctx.Err()
		}
	}
	return PublicationState{Found: true, State: "PUBLISHED", GameID: id}, nil
}

func TestPublicationDoesNotSerializeUnrelatedItems(t *testing.T) {
	files, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		repository := &blockedPublicationRepository{blocked: make(chan struct{}), calls: map[string]int{}}
		defer close(repository.blocked)
		first := NewReviewApprovals(repository, nil, time.Now, files)
		second := NewReviewApprovals(repository, nil, time.Now, files)
		go func() { _, _ = first.Approve(t.Context(), ReviewApprovalRequest{ItemID: "slow", ExpectedVersion: 1}) }()
		synctest.Wait()
		done := make(chan error, 1)
		go func() {
			_, err := second.Approve(t.Context(), ReviewApprovalRequest{ItemID: "fast", ExpectedVersion: 1})
			done <- err
		}()
		synctest.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("unrelated publication waits for the slow item")
		}
	})
}

func TestPublicationSerializesSameItemAndCancelsWaiter(t *testing.T) {
	files, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		repository := &blockedPublicationRepository{blocked: make(chan struct{}), calls: map[string]int{}}
		defer close(repository.blocked)
		service := NewReviewApprovals(repository, nil, time.Now, files)
		request := ReviewApprovalRequest{ItemID: "slow", ExpectedVersion: 1}
		go func() { _, _ = service.Approve(t.Context(), request) }()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := service.Approve(ctx, request); done <- err }()
		synctest.Wait()
		if repository.calls["slow"] != 1 {
			t.Fatal("same item entered publication concurrently")
		}
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error = %v", err)
			}
		default:
			t.Fatal("canceled publication waiter remains blocked")
		}
	})
}
