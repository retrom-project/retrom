package store

import (
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestApplicationWriteQueriesReferenceCurrentSchema(t *testing.T) {
	t.Parallel()
	db := lifecycleDatabase(t)
	files, err := filepath.Glob(filepath.Join("..", "recordstore", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			query, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			query = strings.TrimSpace(query)
			if !strings.HasPrefix(query, "WITH previous(") && !strings.HasPrefix(query, "SELECT CASE") {
				return true
			}
			checked++
			checkApplicationWriteQuery(t, db, path, query)
			return true
		})
	}
	if checked == 0 {
		t.Fatal("no application write queries checked")
	}
}

func checkApplicationWriteQuery(t *testing.T, db *sql.DB, path, query string) {
	t.Helper()
	args := make([]any, strings.Count(query, "?"))
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Errorf("%s: %v", path, err)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Error(err)
	}
}
