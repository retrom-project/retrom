//go:build integration

package launch

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"modernc.org/sqlite"

	"retrom/internal/capability/content/contentcapability"
	launchmodel "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
	persistence "retrom/internal/repo/launch"
	launchservice "retrom/internal/service/launch"
)

func productValidationInput(t *testing.T, fixture reviewCheckpointFixture, request CreateRequest) launchmodel.ValidationInputs {
	t.Helper()
	var input launchmodel.ValidationInputs
	err := fixture.database.QueryRowContext(t.Context(), `SELECT variant.id,variant.provider_id,variant.target_id,game.version,game.source_manifest_digest
FROM game_variants variant JOIN games game ON game.id=variant.game_id WHERE game.id=?`, request.GameID).Scan(&input.GameVariantID, &input.ProviderID, &input.TargetID, &input.GameVersion, &input.SourceManifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	input.GameID = request.GameID
	input.ContentPolicy = contentcapability.NewPolicy("RPG_MAKER_PROJECT")
	input.ValidationInputDigest = "test-validation-digest"
	input.BIOSDependencyDigest = "test-bios-digest"
	return input
}

func productValidationRows(t *testing.T, fixture reviewCheckpointFixture) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{"jobs", "job_input_snapshots", "job_events", "game_variants"} {
		var value string
		query := `SELECT COALESCE(json_group_array(json(row_json)),'[]') FROM (SELECT json_object(`
		columns, err := fixture.database.QueryContext(t.Context(), `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
		if err != nil {
			t.Fatal(err)
		}
		first := true
		for columns.Next() {
			var name string
			if err := columns.Scan(&name); err != nil {
				t.Fatal(err)
			}
			if !first {
				query += ","
			}
			first = false
			query += "'" + name + "',\"" + name + "\""
		}
		if err := columns.Err(); err != nil {
			t.Fatal(err)
		}
		if err := columns.Close(); err != nil {
			t.Fatal(err)
		}
		query += `) AS row_json FROM ` + table + ` ORDER BY rowid)`
		if err := fixture.database.QueryRowContext(t.Context(), query).Scan(&value); err != nil {
			t.Fatal(err)
		}
		result[table] = value
	}
	return result
}

func TestProductValidationQueueRollsBackAllWritesAfterEventFailure(t *testing.T) {
	fixture, request := productCreationFixture(t)
	input := productValidationInput(t, fixture, request)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE job_events ADD COLUMN product_queue_guard INTEGER CHECK(scope_type!='GAME_VARIANT' OR event_type!='QUEUED')`)
	before := productValidationRows(t, fixture)
	tx, err := fixture.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := launchservice.NewValidationScheduler(persistence.NewValidationJobs(tx), launchservice.ValidationEnvironment{Now: fixture.launcher.now})
	result, err := scheduler.Queue(t.Context(), input)
	var storage *sqlite.Error
	if !errors.As(err, &storage) || result.JobID != "" {
		dbexec.Rollback(tx)
		t.Fatalf("result=%+v error=%v", result, err)
	}
	dbexec.Rollback(tx)
	if actual := productValidationRows(t, fixture); !reflect.DeepEqual(before, actual) {
		t.Fatal("failed event left job/input/variant writes")
	}
}

func TestProductValidationConcurrentSameDigestUsesOneJob(t *testing.T) {
	fixture, request := productCreationFixture(t)
	input := productValidationInput(t, fixture, request)
	type outcome struct {
		result launchmodel.ValidationQueued
		err    error
	}
	outcomes := make(chan outcome, 2)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			result, err := queueProductValidation(t.Context(), fixture, input)
			outcomes <- outcome{result, err}
		}()
	}
	ready.Wait()
	close(start)
	first, second := <-outcomes, <-outcomes
	if first.err != nil || second.err != nil || first.result.JobID == "" || first.result.JobID != second.result.JobID || first.result.Queued == second.result.Queued {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	var counts string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT json_array((SELECT count(*) FROM jobs WHERE id=?),(SELECT count(*) FROM job_input_snapshots WHERE job_id=?),(SELECT count(*) FROM job_events WHERE job_id=?))`, first.result.JobID, first.result.JobID, first.result.JobID).Scan(&counts); err != nil {
		t.Fatal(err)
	}
	var values []int
	if err := json.Unmarshal([]byte(counts), &values); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(values, []int{1, 1, 1}) {
		t.Fatalf("concurrent queue counts=%v", values)
	}
}

func queueProductValidation(ctx context.Context, fixture reviewCheckpointFixture, input launchmodel.ValidationInputs) (launchmodel.ValidationQueued, error) {
	tx, err := fixture.database.BeginTx(ctx, nil)
	if err != nil {
		return launchmodel.ValidationQueued{}, err
	}
	defer dbexec.Rollback(tx)
	result, err := launchservice.NewValidationScheduler(persistence.NewValidationJobs(tx), launchservice.ValidationEnvironment{Now: fixture.launcher.now}).Queue(ctx, input)
	if err != nil {
		return launchmodel.ValidationQueued{}, err
	}
	if err := tx.Commit(); err != nil {
		return launchmodel.ValidationQueued{}, err
	}
	return result, nil
}
