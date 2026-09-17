package accounts

import (
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/model/accounts"
	"retrom/internal/testkit/testsupport"
)

func TestAccountAndIPRateLimitRecordedAtomically(t *testing.T) {
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
	_, err = repository.CommitRecordRateLimit(t.Context(), accounts.RecordRateLimitCommand{
		Entries: []accounts.RateLimitEntry{
			{Key: accounts.RateLimitKey{Scope: "LOGIN_ACCOUNT"}, Threshold: 10},
			{Key: accounts.RateLimitKey{Scope: "LOGIN_IP"}, Threshold: 10},
		},
		NowMS: 100,
	})
	if err != nil {
		t.Fatalf("rate limit write: %v", err)
	}
	for _, scope := range []string{"LOGIN_ACCOUNT", "LOGIN_IP"} {
		_, found, err := repository.Read(t.Context(), accounts.RateLimitKey{Scope: scope})
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatalf("%s bucket not committed", scope)
		}
	}
}
