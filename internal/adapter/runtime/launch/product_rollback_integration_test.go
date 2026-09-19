//go:build integration

package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"modernc.org/sqlite"

	"retrom/internal/foundation/cleanup"
	launchmodel "retrom/internal/model/launch"
	persistence "retrom/internal/repo/launch"
)

type productWriteFaultRepository struct {
	launchmodel.ProductCreationRepository

	change  func(*launchmodel.ProductCreatePlan)
	after   func() error
	reached bool
}

func (repository *productWriteFaultRepository) WithCreation(ctx context.Context, work func(launchmodel.ProductCreationScope) error) error {
	return repository.ProductCreationRepository.WithCreation(ctx, func(scope launchmodel.ProductCreationScope) error {
		if err := work(productWriteFaultScope{ProductCreationScope: scope, change: repository.change}); err != nil {
			return err
		}
		repository.reached = true
		if repository.after != nil {
			return repository.after()
		}
		return nil
	})
}

type productWriteFaultScope struct {
	launchmodel.ProductCreationScope

	change func(*launchmodel.ProductCreatePlan)
}

func (scope productWriteFaultScope) Create(ctx context.Context, plan launchmodel.ProductCreatePlan) error {
	if scope.change != nil {
		scope.change(&plan)
	}
	return scope.ProductCreationScope.Create(ctx, plan)
}

func productCreationRows(t *testing.T, database *sql.DB) map[string]string {
	t.Helper()
	result := playRows(t, database)
	for _, table := range []string{"launch_content_files", "launch_external_files", "idempotency_records", "variant_files"} {
		encoded := productTableRows(t, database, table)
		result[table] = string(encoded)
	}
	for table, value := range productValidationRows(t, reviewCheckpointFixture{database: database}) {
		result[table] = value
	}
	return result
}

func assertProductCreationRollback(t *testing.T, service *Service, command launchmodel.ProductCreateCommand) {
	t.Helper()
	cause := errors.New("product interrupted after all writes and receipt")
	repository := &productWriteFaultRepository{ProductCreationRepository: persistence.NewProductCreation(service.database), after: func() error { return cause }}
	before := productCreationRows(t, service.database)
	result, err := service.productCreator(repository).Create(t.Context(), command)
	if !errors.Is(err, cause) || result.Created.LaunchID != "" || len(result.Body) != 0 || !repository.reached {
		t.Fatalf("post-write error=%v reached=%v launch=%q bodyBytes=%d", err, repository.reached, result.Created.LaunchID, len(result.Body))
	}
	if !reflect.DeepEqual(before, productCreationRows(t, service.database)) {
		t.Fatal("post-write rollback retained product owners, files, tickets or receipt")
	}
	repository.after = nil
	repository.change = func(plan *launchmodel.ProductCreatePlan) {
		plan.External = append(plan.External, launchmodel.ProductExternalFile{Kind: "BIOS", LogicalName: "late-failure.bin", VirtualPath: "/late-failure.bin", BlobID: "absent"})
	}
	result, err = service.productCreator(repository).Create(t.Context(), command)
	var storage *sqlite.Error
	if !errors.As(err, &storage) || result.Created.LaunchID != "" || len(result.Body) != 0 {
		t.Fatalf("late write launch=%q bodyBytes=%d error=%v", result.Created.LaunchID, len(result.Body), err)
	}
	if !reflect.DeepEqual(before, productCreationRows(t, service.database)) {
		t.Fatal("late file failure retained product writes")
	}
}

func TestProductCreationRollsBackEveryOwner(t *testing.T) {
	fixture, request := productCreationFixture(t)
	assertProductCreationRollback(t, fixture.launcher, launchmodel.ProductCreateCommand{ProfileID: "local", Request: request})
}

func productTableRows(t *testing.T, database *sql.DB, table string) []byte {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), `SELECT * FROM `+table+` ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close product table snapshot", rows.Close()) }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var values [][]any
	for rows.Next() {
		cells, pointers := make([]any, len(columns)), make([]any, len(columns))
		for index := range cells {
			pointers[index] = &cells[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		values = append(values, cells)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
