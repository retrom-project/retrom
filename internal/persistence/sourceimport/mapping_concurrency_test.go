package sourceimport

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	tagrepository "retrom/internal/persistence/tagging"
	application "retrom/internal/service/sourceimport"
	"retrom/internal/service/tagging"
)

type interleavedMappings struct {
	repository  *Mappings
	db          dbapi.DB
	interleaves int
}

func (r *interleavedMappings) WithMappings(ctx context.Context, work func(application.MappingScope) error) error {
	return r.repository.WithMappings(ctx, func(scope application.MappingScope) error {
		scope.Write = interleavedAdvance{MappingWriter: scope.Write, before: func() error {
			r.interleaves++
			return dbapi.RetryTransaction(ctx, r.db, func(tx dbapi.Tx) error {
				if _, err := (&Queries{database: tx}).Get(ctx, "import-1"); err != nil {
					return err
				}
				_, err := tx.ExecContext(ctx, `UPDATE source_imports
SET version=version+1,updated_at_ms=updated_at_ms+1 WHERE id='import-1'`)
				return err
			})
		}}
		return work(scope)
	})
}

type interleavedAdvance struct {
	application.MappingWriter
	before func() error
}

func (w interleavedAdvance) Advance(ctx context.Context, change application.MappingAdvance) error {
	if err := w.before(); err != nil {
		return fmt.Errorf("unrelated progress transaction: %w", err)
	}
	return w.MappingWriter.Advance(ctx, change)
}

func concurrentMappingBatch(t *testing.T, db dbapi.DB, instance string) []application.Mapping {
	t.Helper()
	plan := creationPlan(1)
	plan.ActorID = mappingActor
	if err := NewCreation(db).WithCreate(t.Context(), func(w application.CreationWriter) error {
		_, err := w.Insert(t.Context(), plan)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mappings := []application.Mapping{{
		CollectionID: mappingCollection, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{},
	}}
	for n := 1; n < 100; n++ {
		id := fmt.Sprintf("019b0000-0000-7000-8001-%012d", n)
		if _, err := db.ExecContext(t.Context(), `
INSERT INTO source_import_collections(id,import_id,metadata_relative_path,segment_ordinal,name,
game_count,created_at_ms,updated_at_ms) VALUES(?,'import-0',?,0,'Collection',0,1,1)`, id, fmt.Sprintf("game-%d/metadata.pegasus.txt", n)); err != nil {
			t.Fatal(err)
		}
		mappings = append(mappings, application.Mapping{
			CollectionID: id, Action: "IMPORT", PlatformInstanceID: instance, TagIDs: []string{},
		})
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE source_imports SET collection_count=100 WHERE id='import-0'`); err != nil {
		t.Fatal(err)
	}
	return mappings
}

func TestUnrelatedSourceProgressDoesNotBreakMappings(t *testing.T) {
	db := mappingDatabase(t)
	instance := seedMappingTarget(t, db)
	mappings := concurrentMappingBatch(t, db, instance)
	repository := &interleavedMappings{repository: NewMappings(db), db: db}
	service := application.NewMappings(repository, tagging.New(tagrepository.New(db), time.Now), time.Now)
	started := time.Now()
	result, err := service.Update(t.Context(), "import-0", 1, mappings, mappingActor)
	t.Logf("collections=100 unrelated_source_transactions=%d duration=%v error=%v",
		repository.interleaves, time.Since(started), err)
	if err != nil {
		t.Fatalf("mapping one Source failed when another Source alone changed: %v", err)
	}
	if result.Version != 2 || result.MappingVersion != 2 || result.Counts.MappedCollections != 100 || result.Retryable {
		t.Fatalf("unexpected committed mapping: %#v", result)
	}
	if repository.interleaves != 1 {
		t.Fatalf("unrelated progress caused %d attempts", repository.interleaves)
	}
	before := mappingRows(t, db)
	_, err = service.Update(t.Context(), "import-0", 1, mappings, mappingActor)
	if !errors.Is(err, application.ErrVersionConflict) {
		t.Fatalf("stale mapping: %v", err)
	}
	if !reflect.DeepEqual(before, mappingRows(t, db)) {
		t.Fatal("stale mapping left partial writes")
	}
}

func TestMappingsLockTheirSourceAndSelectedTarget(t *testing.T) {
	db := mappingDatabase(t)
	instance := seedMappingTarget(t, db)
	err := NewMappings(db).WithMappings(t.Context(), func(scope application.MappingScope) error {
		if _, err := scope.Read.Import(t.Context(), "import-0"); err != nil {
			return err
		}
		if _, found, err := scope.Read.EligibleTarget(t.Context(), instance); err != nil || !found {
			t.Fatalf("target: found=%v error=%v", found, err)
		}
		for _, query := range []struct{ sql, id string }{
			{"SELECT id FROM source_imports WHERE id=? FOR UPDATE NOWAIT", "import-0"},
			{"SELECT id FROM platform_instances WHERE id=? FOR UPDATE NOWAIT", instance},
		} {
			err := dbapi.InTransaction(t.Context(), db, nil, func(tx dbapi.Tx) error {
				var id string
				return dbapi.QueryRowContext(t.Context(), tx, query.sql, query.id).Scan(&id)
			})
			var state interface{ SQLState() string }
			if !errors.As(err, &state) || state.SQLState() != "55P03" {
				t.Fatalf("concurrent writer was not fenced: %v", err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
