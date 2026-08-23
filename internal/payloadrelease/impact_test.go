package payloadrelease

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestImpactSourceKindsIncludeEmulationStationAndNeverReturnNull(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"SERVER_PEGASUS_IMPORT", "SERVER_EMULATIONSTATION_IMPORT"} {
		normalized, ok := normalizedImpactSourceKind(source)
		testassert.Truef(t, ok && normalized == "SERVER_SCAN", "%s normalized to %q", source, normalized)
	}

	database, err := testsupport.OpenDatabase(
		context.Background(),
		filepath.Join(t.TempDir(), "retrom.db"),
		time.Now,
	)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	transaction, err := database.SQL.BeginTx(context.Background(), nil)
	testassert.False(t, err != nil, err)
	defer cleanup.Rollback(transaction)
	kinds, err := impactSourceKinds(context.Background(), transaction, "missing-game")
	testassert.False(t, err != nil, err)
	testassert.Truef(t, kinds != nil && len(kinds) == 0, "empty source kinds = %#v", kinds)
}
