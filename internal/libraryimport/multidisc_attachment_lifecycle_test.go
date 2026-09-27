package libraryimport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	dbapi "retrom/internal/database"
)

var errAttachmentTestStopped = errors.New("attachment test stopped")

type admittedAttachment struct{}

func (admittedAttachment) Create(context.Context, string, int64, MultiDiscAttachmentRequest) (MultiDiscAttachmentCreated, error) {
	return MultiDiscAttachmentCreated{JobID: "job"}, nil
}

type attachmentClaimGate struct {
	dbapi.DB
	entered, cancelled, release chan struct{}
	once                        sync.Once
}

func (gate *attachmentClaimGate) unblock() { gate.once.Do(func() { close(gate.release) }) }
func (gate *attachmentClaimGate) BeginTx(ctx context.Context, _ *dbapi.TxOptions) (dbapi.Tx, error) {
	close(gate.entered)
	select {
	case <-ctx.Done():
		close(gate.cancelled)
		<-gate.release
		return nil, ctx.Err()
	case <-gate.release:
		return nil, errAttachmentTestStopped
	}
}

func TestNewMultiDiscAttachmentBelongsToImporterLifetime(t *testing.T) {
	gate := &attachmentClaimGate{entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	service := &Service{database: gate, now: time.Now, attachmentCreator: admittedAttachment{}}
	t.Cleanup(func() { gate.unblock(); service.Close() })
	if _, err := service.CreateMultiDiscAttachment(t.Context(), "item", 1, MultiDiscAttachmentRequest{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("attachment did not reach claim")
	}
	closed := make(chan struct{})
	go func() { service.Close(); close(closed) }()
	select {
	case <-gate.cancelled:
	case <-closed:
		t.Fatal("Close returned without canceling the new attachment")
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel the attachment")
	}
	select {
	case <-closed:
		t.Fatal("Close returned before attachment exited")
	default:
	}
	gate.unblock()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join attachment")
	}
}
