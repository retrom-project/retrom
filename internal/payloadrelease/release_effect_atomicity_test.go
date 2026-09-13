package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

type effectWriteFailure struct{ prefix, identity string }

func TestReleaseGameGraphWritesRollBackTogether(t *testing.T) {
	for _, stage := range []effectWriteFailure{
		{"DELETE FROM game_assets", "schedule-game"},
		{"UPDATE upload_files", "effect-upload"},
		{"INSERT INTO jobs", "effect-blob"},
		{"INSERT INTO job_input_snapshots", `"id":"effect-blob"`},
		{"INSERT INTO job_events", "effect-blob"},
		{"INSERT INTO blob_gc_candidates", "effect-blob"},
		{"UPDATE games SET payload_state='RELEASED'", "schedule-game"},
	} {
		for _, mode := range []string{"sql", "count", "zero"} {
			t.Run(stage.prefix+"/"+mode, func(t *testing.T) {
				fixture := queuedReleaseWorker(t)
				seedEffectGamePayload(t, fixture.database)
				claim := claimEffect(t, fixture)
				cause := errors.New("release graph write failed")
				var hits atomic.Int64
				service := effectFaultService(t, fixture, effectGraphWriteFault(stage, mode, cause, &hits))
				err := service.execute(t.Context(), claim)
				if hits.Load() != 1 || err == nil || mode != "zero" && !errors.Is(
					err,
					cause,
				) {
					t.Fatalf(
						"graph error=%v hits=%d",
						err,
						hits.Load(),
					)
				}
				assertEffectGameGraphRetained(t, fixture)
				if err := fixture.service.execute(t.Context(), claim); err != nil {
					t.Fatal(err)
				}
				assertEffectGameGraphReleased(t, fixture)
			})
		}
	}
}

func effectGraphWriteFault(
	stage effectWriteFailure,
	mode string,
	cause error,
	hits *atomic.Int64,
) testsupport.SQLFaultHooks {
	return testsupport.SQLFaultHooks{
		AfterExec: func(_ context.Context, query string, args []driver.NamedValue, result driver.Result) (driver.Result, error) {
			if !strings.HasPrefix(
				strings.Join(strings.Fields(query), " "),
				stage.prefix,
			) || !effectGraphArgument(
				args,
				stage.identity,
			) {
				return result, nil
			}
			hits.Add(1)
			switch mode {
			case "count":
				return failedSchedulingCount{Result: result, cause: cause}, nil
			case "zero":
				return driver.RowsAffected(0), nil
			default:
				return result, cause
			}
		},
	}
}

func effectGraphArgument(args []driver.NamedValue, identity string) bool {
	for _, arg := range args {
		value, ok := arg.Value.(string)
		if ok && (value == identity || strings.HasPrefix(identity, `"id":`) && strings.Contains(value, identity)) {
			return true
		}
	}
	return false
}
