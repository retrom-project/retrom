package libraryimport

import (
	"context"
	"testing"
	"time"
)

func TestAttachmentShutdownCancelsActiveWorkAndDelayedRetries(t *testing.T) {
	service := &Service{}
	started, ended, delayed := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	service.scheduleAttachment(t.Context(), 0, func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(ended)
	})
	service.scheduleAttachment(t.Context(), time.Hour, func(context.Context) { delayed <- struct{}{} })
	<-started
	service.Close()
	select {
	case <-ended:
	default:
		t.Fatal("Close returned while attachment work was active")
	}
	service.scheduleAttachment(t.Context(), 0, func(context.Context) { delayed <- struct{}{} })
	service.Close()
	select {
	case <-delayed:
		t.Fatal("shutdown allowed a delayed or new attachment to run")
	default:
	}
}
