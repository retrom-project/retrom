package architecture

import (
	"testing"
)

func TestSQLExecutionThroughMethodValueIsForbiddenOutsideRepo(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "internal/service/value/service.go", `package value
import ("context"; db "database/sql")
func Write(ctx context.Context, connection *db.DB) error {
	execute := connection.ExecContext
	_, err := execute(ctx, "DELETE FROM records")
	return err
}
`)
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/service/value", Layer: "service", Module: "value"},
	}}
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	violations := inspectExecutionRules(inspectFunctionGraph(root, graph, owners))
	if len(violations) != 1 || violations[0].Rule != "AR06" || violations[0].Line != 4 {
		t.Fatalf("SQL method value escaped its defining line: %+v", violations)
	}
}

func TestUnclassifiedExternalCallDoesNotPassPurity(t *testing.T) {
	t.Parallel()
	functions := []FunctionInventory{{
		Symbol: "retrom/internal/model/value.Token", File: "internal/model/value/value.go", Layer: "model",
		Calls: []CallInventory{{Symbol: "example.org/random.New", Package: "example.org/random", Line: 3}},
	}}
	violations := inspectExecutionRules(functions)
	if len(violations) != 1 || violations[0].Message != "unclassified external call is forbidden in model" {
		t.Fatalf("unknown library silently assumed pure: %+v", violations)
	}
}
