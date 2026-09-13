package metadatascrape

import (
	"testing"

	"retrom/internal/hasheous"
)

func TestProviderResponseExpiryUsesPersistedFetchTime(t *testing.T) {
	if got := ResponseExpiry(hasheous.OutcomeMiss, 100); got != 100+24*60*60*1000 {
		t.Fatalf("cache expiry used a second clock read: %d", got)
	}
}
