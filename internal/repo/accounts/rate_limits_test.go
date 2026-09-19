package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	accountsmodel "retrom/internal/model/accounts"
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
	err = repository.WithWrite(t.Context(), func(records accountsmodel.RateLimitRecords) error {
		for _, scope := range []string{"LOGIN_ACCOUNT", "LOGIN_IP"} {
			if err := records.Write(t.Context(), accountsmodel.RateLimitBucket{Key: accountsmodel.RateLimitKey{Scope: scope}, WindowStarted: 100, Failures: 1, UpdatedAt: 100}); err != nil {
				return err
			}
		}
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("rate limit write failure: %v", err)
	}
	for _, scope := range []string{"LOGIN_ACCOUNT", "LOGIN_IP"} {
		_, found, err := repository.Read(t.Context(), accountsmodel.RateLimitKey{Scope: scope})
		if err != nil {
			t.Fatal(err)
		}
		if found {
			t.Fatalf("partial %s failure bucket committed", scope)
		}
	}
}
