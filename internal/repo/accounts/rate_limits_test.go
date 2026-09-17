package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/model/accounts"
	"retrom/internal/testkit/testsupport"
)

func TestAccountAndIPRateLimitFailuresRollbackTogether(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := NewRateLimits(database.SQL)
	err = repository.CommitWrite(t.Context(), func(records accounts.RateLimitRecords) error {
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
