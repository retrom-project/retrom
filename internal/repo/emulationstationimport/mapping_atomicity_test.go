package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	taggingmodel "retrom/internal/model/tagging"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

var errMappingStep = errors.New("mapping step failed")

type mappingFaultRepository struct {
	repository *Mappings
	phase      string
}

func (r mappingFaultRepository) WithMappings(ctx context.Context, work func(emulationstationimportmodel.MappingScope) error) error {
	return r.repository.WithMappings(ctx, func(scope emulationstationimportmodel.MappingScope) error {
		scope.Read = &mappingFaultReader{MappingReader: scope.Read, fail: r.phase == "response"}
		scope.Write = &mappingFaultWriter{MappingWriter: scope.Write, phase: r.phase}
		if r.phase == "tag touch" {
			scope.Tags = &mappingFaultTagWriter{CrossDomainWriter: scope.Tags}
		}
		return work(scope)
	})
}

type mappingFaultReader struct {
	emulationstationimportmodel.MappingReader
	fail  bool
	reads int
}

func (reader *mappingFaultReader) Import(ctx context.Context, id string) (emulationstationimportmodel.Summary, error) {
	reader.reads++
	result, err := reader.MappingReader.Import(ctx, id)
	if err != nil {
		return result, err
	}
	if reader.fail && reader.reads > 1 {
		return emulationstationimportmodel.Summary{}, errMappingStep
	}
	return result, nil
}

type mappingFaultWriter struct {
	emulationstationimportmodel.MappingWriter
	phase  string
	writes int
}

func (writer *mappingFaultWriter) Put(ctx context.Context, change emulationstationimportmodel.CollectionMapping) error {
	writer.writes++
	if writer.phase == "collection SQL" || writer.phase == "second collection SQL" && writer.writes == 2 {
		change.Mapping.Action = "INVALID_ACTION"
	}
	return writer.MappingWriter.Put(ctx, change)
}

func (writer *mappingFaultWriter) Advance(ctx context.Context, change emulationstationimportmodel.MappingAdvance) error {
	switch writer.phase {
	case "aggregate CAS":
		change.Before.Version++
	case "mapping CAS":
		change.Before.MappingVersion++
	}
	if err := writer.MappingWriter.Advance(ctx, change); err != nil {
		return err
	}
	if writer.phase == "after aggregate" {
		return errMappingStep
	}
	return nil
}

type mappingFaultTagWriter struct{ taggingmodel.CrossDomainWriter }

func (w *mappingFaultTagWriter) ReplaceOwnerReferences(
	ctx context.Context, owner taggingmodel.Owner, tagIDs []string,
	actorUserID string, now int64,
) ([]taggingmodel.Reference, []taggingmodel.Reference, error) {
	before, after, err := w.CrossDomainWriter.ReplaceOwnerReferences(ctx, owner, tagIDs, actorUserID, now)
	if err != nil {
		return nil, nil, err
	}
	return before, after, errMappingStep
}

func TestMappingsRollbackEveryProjectionAtEachWriteBoundary(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"tag touch", "collection SQL", "second collection SQL", "aggregate CAS", "mapping CAS", "after aggregate", "response"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			db := mappingDatabase(t)
			seedSecondMappingCollection(t, db)
			before := planRows(t, db)
			service := emulationstationimportservice.NewMappings(mappingFaultRepository{repository: NewMappings(db), phase: phase}, func() time.Time { return time.UnixMilli(10) })
			result, err := service.Update(t.Context(), "import-0", 1, []emulationstationimportmodel.Mapping{
				{CollectionID: mappingCollection, Action: "SKIP", TagIDs: []string{}},
				{CollectionID: secondMappingCollection, Action: "SKIP", TagIDs: []string{}},
			}, mappingActor)
			assertMappingStepFailure(t, phase, result, err)
			if !reflect.DeepEqual(planRows(t, db), before) {
				t.Fatal("failure changed persisted plan, collections, tags or immutable evidence")
			}
		})
	}
}

func assertMappingStepFailure(t *testing.T, phase string, result emulationstationimportmodel.Summary, err error) {
	t.Helper()
	if err == nil || result.ID != "" {
		t.Fatalf("%s returned success or partial result: %#v error=%v", phase, result, err)
	}
	switch phase {
	case "collection SQL", "second collection SQL":
		if errors.Is(err, emulationstationimportmodel.ErrInvalid) {
			t.Fatalf("SQL cause replaced with input error: %v", err)
		}
	case "aggregate CAS", "mapping CAS":
		if !errors.Is(err, emulationstationimportmodel.ErrVersionConflict) {
			t.Fatalf("CAS cause lost: %v", err)
		}
	default:
		if !errors.Is(err, errMappingStep) {
			t.Fatalf("callback cause lost: %v", err)
		}
	}
}
