package httpapi

import (
	"testing"

	dbapi "retrom/internal/database"
)

func TestGameCoreOptionsProjectBoundTargetContentLoading(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	ctx := t.Context()
	transaction, err := server.database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(transaction)
	const gameID = "01980000-0000-7000-8000-000000000201"
	const variantID = "01980000-0000-7000-8000-000000000206"
	fixture := gameDetailSeed{now: 1750000000000}
	seedGameDetailMedia(t, server, transaction, gameID,
		"01980000-0000-7000-8000-000000000202", "01980000-0000-7000-8000-000000000203",
		"01980000-0000-7000-8000-000000000204", "01980000-0000-7000-8000-000000000205",
		"01980000-0000-7000-8000-000000000210", &fixture)
	seedGameDetailRuntime(t, server, transaction, gameID, variantID,
		"01980000-0000-7000-8000-000000000208", &fixture)
	for _, capability := range []string{"ON_DEMAND_AND_PRELOAD", "PRELOAD_ONLY", ""} {
		t.Run("capability="+capability, func(t *testing.T) {
			query := `UPDATE runtime_targets SET capabilities_json=(jsonb_set((capabilities_json)::jsonb,'{contentLoading}',to_jsonb((?)::text),true))::text
 WHERE (provider_id,target_id) IN (SELECT provider_id,target_id FROM game_variants WHERE id=?)`
			if capability == "" {
				query = `UPDATE runtime_targets SET capabilities_json=(capabilities_json::jsonb - 'contentLoading')::text
 WHERE (provider_id,target_id) IN (SELECT provider_id,target_id FROM game_variants WHERE id=?) AND ?=''`
				if _, err := server.database.ExecContext(ctx, query, variantID, capability); err != nil {
					t.Fatal(err)
				}
			} else if _, err := server.database.ExecContext(ctx, query, capability, variantID); err != nil {
				t.Fatal(err)
			}
			options, err := server.gameCoreOptions(ctx, gameID)
			if err != nil {
				t.Fatal(err)
			}
			if len(options) != 1 {
				t.Fatalf("core options = %#v", options)
			}
			var expected any
			if capability != "" {
				expected = capability
			}
			if options[0]["contentLoading"] != expected {
				t.Fatalf("loading capability = %#v, want %#v", options[0]["contentLoading"], expected)
			}
		})
	}
}
