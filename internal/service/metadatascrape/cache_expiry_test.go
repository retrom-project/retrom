package metadatascrape

import (
	"testing"

	metadatamodel "retrom/internal/model/metadata"
)

func TestProviderResponseExpiryUsesPersistedFetchTime(t *testing.T) {
	if got := ResponseExpiry(metadatamodel.OutcomeMiss, 100); got != 100+24*60*60*1000 {
		t.Fatalf("cache expiry used a second clock read: %d", got)
	}
}
