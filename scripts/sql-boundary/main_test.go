package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTypedSQLBoundary(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module retrom\n\ngo 1.26.5\n")
	write("internal/persistence/repository.go", `package persistence
type Repository struct{}
func (*Repository) Count(string) int {return 0}
func (*Repository) Execute(string) {}
func (*Repository) Games() {}`)
	cases := []struct {
		name string
		body string
		bad  bool
	}{
		{"direct", `r.Count("SELECT 1")`, true},
		{"alias", `alias := r; alias.Execute("SELECT 1")`, true},
		{"method-value", `execute := r.Execute; execute("SELECT 1")`, true},
		{"method-expression", `execute := (*persistence.Repository).Execute; execute(r,"SELECT 1")`, true},
		{"named-operation", `r.Games()`, false},
		{"unrelated-count", `_ = strings.Count("a/b","/"); r.Games()`, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			write("internal/service/example/service.go", `package example
import ("retrom/internal/persistence"; "strings")
var _ = strings.Count
func run(r *persistence.Repository) {`+item.body+`}`)
			findings, err := check(root, "./internal/service/...")
			if err != nil || (len(findings) != 0) != item.bad {
				t.Fatalf("findings=%v expected violation=%v err=%v", findings, item.bad, err)
			}
		})
	}
}
