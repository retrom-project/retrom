package architecture

import (
	"strings"
	"testing"
)

func TestSQLInventoryResolvesConstantsButRetainsUnknownQueries(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "internal/repo/value/queries.go", `package value
import ("context"; "database/sql")
const table = "records"
func Static(ctx context.Context, db *sql.DB) { _, _ = db.ExecContext(ctx, "DELETE FROM "+table) }
func Dynamic(ctx context.Context, db *sql.DB, query string) { _, _ = db.ExecContext(ctx, query) }
func MethodExpression(ctx context.Context, db *sql.DB) { _, _ = (*sql.DB).ExecContext(db, ctx, "DELETE FROM records") }
`)
	owners := OwnershipRegistry{Packages: []PackageOwnership{{Path: "internal/repo/value", Layer: "repo"}}}
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	functions := inspectFunctionGraph(root, graph, owners)
	queries := make(map[string]SQLInventory)
	for _, function := range functions {
		for _, call := range function.Calls {
			if call.SQL != nil {
				queries[function.Symbol] = *call.SQL
			}
		}
	}
	if len(queries) != 3 {
		t.Fatalf("missing executed SQL: %+v", queries)
	}
	for symbol, query := range queries {
		dynamic := strings.HasSuffix(symbol, ".Dynamic")
		if dynamic && query.Constant {
			t.Fatalf("dynamic SQL falsely resolved: %+v", query)
		}
		if !dynamic && (!query.Constant || query.Text != "DELETE FROM records") {
			t.Fatalf("constant SQL lost or shifted method receiver: %s %+v", symbol, query)
		}
	}
}
