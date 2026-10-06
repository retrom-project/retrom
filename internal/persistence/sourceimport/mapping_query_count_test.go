package sourceimport

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	tagrepository "retrom/internal/persistence/tagging"
	application "retrom/internal/service/sourceimport"
	"retrom/internal/service/tagging"
)

type (
	mappingQueryCounts   struct{ owners, targets int }
	mappingQueryExecutor struct {
		dbapi.Executor
		counts *mappingQueryCounts
	}
)

func (executor mappingQueryExecutor) QueryContext(ctx context.Context, query string, args ...any) (dbapi.Rows, error) {
	if strings.Contains(query, "FROM source_import_collections WHERE") {
		executor.counts.owners++
	}
	if strings.Contains(query, "FROM platform_instances instance") {
		executor.counts.targets++
	}
	return executor.Executor.QueryContext(ctx, query, args...)
}

type countedMappings struct {
	*Mappings
	counts *mappingQueryCounts
}

func (repository countedMappings) WithMappings(ctx context.Context, work func(application.MappingScope) error) error {
	return repository.Mappings.WithMappings(ctx, func(scope application.MappingScope) error {
		reader, ok := scope.Read.(mappingRecords)
		if !ok {
			return fmt.Errorf("unexpected mapping reader %T", scope.Read)
		}
		reader.executor = mappingQueryExecutor{Executor: reader.executor, counts: repository.counts}
		scope.Read = reader
		return work(scope)
	})
}

func TestMappingReadsScaleWithDistinctTargets(t *testing.T) {
	db := mappingDatabase(t)
	first := seedMappingTarget(t, db)
	var second string
	if err := dbapi.QueryRowContext(t.Context(), db,
		`SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	mappings := concurrentMappingBatch(t, db, first)
	version := int64(1)
	for _, count := range []int{1, 10, 50, 100} {
		for _, targets := range []int{1, 2} {
			for _, tagged := range []bool{false, true} {
				t.Run(fmt.Sprintf("collections=%d/targets=%d/tagged=%t", count, targets, tagged), func(t *testing.T) {
					version = checkMappingReadCounts(t, db, mappings[:count], version, second, targets, tagged)
				})
			}
		}
	}
}

func assertMappingBatchSelection(t *testing.T, db dbapi.DB, batch []application.Mapping) {
	t.Helper()
	for _, mapping := range batch {
		var instance string
		var tagCount int
		if err := dbapi.QueryRowContext(t.Context(), db, `SELECT target_platform_instance_id,
(SELECT count(*) FROM source_collection_tags WHERE collection_id=collection.id)
FROM source_import_collections collection WHERE id=?`, mapping.CollectionID).Scan(&instance, &tagCount); err != nil {
			t.Fatal(err)
		}
		if instance != mapping.PlatformInstanceID || tagCount != len(mapping.TagIDs) {
			t.Fatalf("mapping did not persist selection: instance=%s tags=%d", instance, tagCount)
		}
	}
}

func checkMappingReadCounts(t *testing.T, db dbapi.DB, mappings []application.Mapping, version int64,
	second string, targets int, tagged bool,
) int64 {
	t.Helper()
	batch := append([]application.Mapping(nil), mappings...)
	for index := range batch {
		if targets == 2 && index%2 == 1 {
			batch[index].PlatformInstanceID = second
		}
		if tagged {
			batch[index].TagIDs = []string{mappingTag}
		}
	}
	counts := &mappingQueryCounts{}
	service := application.NewMappings(countedMappings{NewMappings(db), counts},
		tagging.New(tagrepository.New(db), time.Now), time.Now)
	before := db.Stats()
	started := time.Now()
	result, err := service.Update(t.Context(), "import-0", version, batch, mappingActor)
	t.Logf("owners=%d targets=%d sql=%d duration=%v", counts.owners, counts.targets,
		db.Stats().SQLCalls-before.SQLCalls, time.Since(started))
	if err != nil {
		t.Fatal(err)
	}

	if counts.owners != 1 || counts.targets > targets {
		t.Errorf("duplicate mapping reads: owners=%d targets=%d", counts.owners, counts.targets)
	}
	assertMappingBatchSelection(t, db, batch)
	return result.Version
}
