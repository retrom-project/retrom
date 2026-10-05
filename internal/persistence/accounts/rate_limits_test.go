package accounts

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"

	"retrom/internal/service/accounts"
	"retrom/internal/testsupport"
)

func TestRateLimitReadDoesNotQueueBehindBackgroundWriter(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := NewRateLimits(database.ReadOnly, database.SQL)
	tx, err := database.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(tx)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, _, err = repository.Read(ctx, accounts.RateLimitKey{Scope: "LOGIN_ACCOUNT"})
	if err != nil {
		t.Fatalf("rate limit read waited for writer: %v", err)
	}
}

func TestAccountAndIPRateLimitFailuresRollbackTogether(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := NewRateLimits(database.ReadOnly, database.SQL)
	err = repository.WithWrite(t.Context(), func(records accounts.RateLimitRecords) error {
		for _, scope := range []string{"LOGIN_ACCOUNT", "LOGIN_IP"} {
			if err := records.Write(t.Context(), accounts.RateLimitBucket{Key: accounts.RateLimitKey{Scope: scope}, WindowStarted: 100, Failures: 1, UpdatedAt: 100}); err != nil {
				return err
			}
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("rate limit write failure: %v", err)
	}
	for _, scope := range []string{"LOGIN_ACCOUNT", "LOGIN_IP"} {
		_, found, err := repository.Read(t.Context(), accounts.RateLimitKey{Scope: scope})
		if err != nil {
			t.Fatal(err)
		}
		if found {
			t.Fatalf("partial %s failure bucket committed", scope)
		}
	}
}
