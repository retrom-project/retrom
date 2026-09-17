package libraryimport

import (
	"context"
	"errors"
	"reflect"
	model "retrom/internal/model/libraryimport"
	"testing"
)

type preparationCatalogStub struct{ failure error }

func (catalog preparationCatalogStub) ActiveDAT(context.Context, string, string) (string, error) {
	return "", catalog.failure
}

func (catalog preparationCatalogStub) MachineClassification(context.Context, string, string) (string, bool, error) {
	return "", false, catalog.failure
}

func (catalog preparationCatalogStub) ArcadeRequirements(context.Context, string, string) (model.ArcadeCatalogRequirements, error) {
	return model.ArcadeCatalogRequirements{}, catalog.failure
}

func (catalog preparationCatalogStub) MachineRelation(context.Context, string, string) (model.ArcadeMachineRelation, bool, error) {
	return model.ArcadeMachineRelation{}, false, catalog.failure
}

func TestImportPreparationPreservesCatalogFailure(t *testing.T) {
	t.Parallel()
	_, facts, request := admissionServiceFixture()
	cause := errors.New("catalog read unavailable")
	preparation := NewImportPreparation(facts, preparationCatalogStub{failure: cause}, nil, model.ImportPreparationOptions{})
	result, err := preparation.Prepare(t.Context(), request)
	if !errors.Is(err, cause) || !reflect.DeepEqual(result, model.PreparedImport{}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(facts.events) != 0 {
		t.Fatalf("preparation wrote records: %v", facts.events)
	}
}

func TestImportPreparationRetainsResolvedInputAndSourceGroups(t *testing.T) {
	t.Parallel()
	_, facts, request := admissionServiceFixture()
	preparation := NewImportPreparation(facts, preparationCatalogStub{}, nil, model.ImportPreparationOptions{})
	result, err := preparation.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Upload.Version != 3 || result.Target.Version != 2 || result.Target.BindingID != "binding" || result.ContentMode != "STANDARD" {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Sources) != 1 || result.Groups[0].Sources[0].File.ID != "file" {
		t.Fatalf("groups=%+v", result.Groups)
	}
	if len(result.Dispositions) != 1 || result.Dispositions[0].Disposition != "SOURCE" || len(facts.events) != 0 {
		t.Fatalf("result=%+v events=%v", result, facts.events)
	}
}
