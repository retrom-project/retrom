package pegasusimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"retrom/internal/testkit/testsupport"
)

func TestWorkerCloseCancelsAndWaitsForPendingRepositoryOperation(t *testing.T) {
	t.Parallel()
	service := recoveryFixture(t)
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(ctx context.Context, query string, args []driver.NamedValue) error {
			if !strings.Contains(query, "ORDER BY job.leased_until_ms,job.id LIMIT ?") || len(args) != 3 {
				return nil
			}
			entered <- ctx
			<-release
			return errors.New("released maintenance barrier")
		},
	})
	service.Start()
	var operation context.Context
	select {
	case operation = <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("maintenance never started")
	}
	closed := make(chan struct{})
	go func() { service.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned while repository operation still active")
	case <-operation.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel repository context")
	}
	select {
	case <-closed:
		t.Fatal("Close did not wait for cancelled repository operation")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not join worker")
	}
}
