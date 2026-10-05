package dependencies

import (
	"context"
	"errors"
	"testing"
	"time"

	dbpostgres "retrom/internal/database/postgres"
	service "retrom/internal/service/dependencies"
	"retrom/internal/testsupport/testpostgres"
)

func TestCatalogWritersSerializeWithoutBlockingUnrelatedWrites(t *testing.T) {
	database, err := dbpostgres.Open(testpostgres.DSN(t), dbpostgres.Options{MaxOpenConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.ExecContext(t.Context(), `
CREATE TABLE dat_versions(id TEXT PRIMARY KEY,parse_status TEXT,version BIGINT,updated_at_ms BIGINT);
INSERT INTO dat_versions VALUES('dat','PENDING',1,0);
CREATE TABLE activity(value BIGINT);
INSERT INTO activity VALUES(0)`)
	if err != nil {
		t.Fatal(err)
	}
	repository := New(database)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- repository.WithWrite(ctx, func(scope service.WriteScope) error {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			return scope.Catalog.MarkParsing(ctx, "dat", 1)
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, unrelatedErr := database.ExecContext(ctx, "UPDATE activity SET value=value+1")
	waiting, stopWaiting := context.WithTimeout(ctx, 150*time.Millisecond)
	secondEntered := false
	waitErr := repository.WithWrite(waiting, func(service.WriteScope) error {
		secondEntered = true
		return nil
	})
	stopWaiting()
	close(release)
	firstErr := <-first
	if unrelatedErr != nil || firstErr != nil || !errors.Is(waitErr, context.DeadlineExceeded) || secondEntered {
		t.Fatalf("unrelated=%v first=%v waiting=%v secondEntered=%v", unrelatedErr, firstErr, waitErr, secondEntered)
	}
	err = repository.WithWrite(ctx, func(scope service.WriteScope) error {
		version, err := scope.Catalog.Version(ctx, "dat")
		if err == nil && version != 2 {
			t.Errorf("catalog version=%d after prior publisher committed", version)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
