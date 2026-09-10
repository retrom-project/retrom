package httpapi

import (
	"context"
	"testing"
	"time"

	"retrom/internal/cleanup"
)

func TestGameCoreOptionsAllowPendingDependencyRevalidation(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	ctx := context.Background()
	transaction, err := server.database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Rollback(transaction)
	const gameID = "01980000-0000-7000-8000-000000000201"
	const variantID = "01980000-0000-7000-8000-000000000206"
	fixture := gameDetailSeed{now: time.Now().UnixMilli()}
	seedGameDetailMedia(t, server, transaction, gameID,
		"01980000-0000-7000-8000-000000000202",
		"01980000-0000-7000-8000-000000000203",
		"01980000-0000-7000-8000-000000000204",
		"01980000-0000-7000-8000-000000000205",
		"01980000-0000-7000-8000-000000000210", &fixture)
	seedGameDetailRuntime(t, server, transaction, gameID, variantID,
		"01980000-0000-7000-8000-000000000208", &fixture)
	for _, test := range []struct {
		name, status, code, expected string
	}{
		{"current", "READY", "READY", "READY"},
		{"replaced dependency", "BLOCKED", "VALIDATION_PENDING", "NEEDS_VALIDATION"},
		{"missing dependency", "BLOCKED", "LAUNCH_BIOS_MISSING", "DEPENDENCY_MISSING"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := server.database.ExecContext(ctx,
				`UPDATE game_variants SET status=?,compatibility_code=?,version=version+1,
emulator_game_id=CASE WHEN ?='READY' THEN emulator_game_id ELSE NULL END WHERE id=?`,
				test.status, test.code, test.status, variantID); err != nil {
				t.Fatal(err)
			}
			options, err := server.gameCoreOptions(ctx, gameID)
			if err != nil {
				t.Fatal(err)
			}
			if len(options) != 1 || options[0]["status"] != test.expected {
				t.Fatalf("core choices = %#v, expected %s", options, test.expected)
			}
		})
	}
}
