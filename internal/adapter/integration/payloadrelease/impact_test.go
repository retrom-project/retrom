package payloadrelease

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	application "retrom/internal/model/payloadrelease"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/testkit/testassert"
	"retrom/internal/testkit/testsupport"
)

func TestImpactSourceKindsIncludeEmulationStationAndNeverReturnNull(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"SERVER_PEGASUS_IMPORT", "SERVER_EMULATIONSTATION_IMPORT"} {
		normalized := application.NormalizeImpactSourceKinds([]string{source})
		testassert.Truef(t, len(normalized) == 1 && normalized[0] == "SERVER_SCAN", "%s normalized to %q", source, normalized)
	}

	database, err := testsupport.OpenDatabase(
		context.Background(),
		filepath.Join(t.TempDir(), "retrom.db"),
		time.Now,
	)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	result, err := GameDeleteImpact(context.Background(), database.SQL, "missing-game")
	testassert.False(t, err != nil, err)
	testassert.Truef(t, result.SourceKinds != nil && len(result.SourceKinds) == 0, "empty source kinds = %#v", result.SourceKinds)
}
