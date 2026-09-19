package architecture

import (
	"strings"
	"testing"
)

func TestPortsUseActualRepositoryImplementationsAndValueGraphs(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "internal/model/value/ports.go", `package value
import ("context"; "io")
type Pure struct { ID string; Data []byte }
type Scope struct { Apply func() error }
type Hidden struct { Next *Scope }
type Repository interface {
	Commit(context.Context, Hidden) error
	Snapshot(context.Context) (Pure, error)
}
type Content interface {
	Open(context.Context, string) (io.ReadCloser, error)
}
type BadContent interface { Open(context.Context) (struct { Reader io.Reader }, error) }
`)
	writeInventoryFile(t, root, "internal/repo/value/repo.go", `package value
import ("context"; model "retrom/internal/model/value")
type Repository struct{}
func (*Repository) Commit(context.Context, model.Hidden) error { return nil }
func (*Repository) Snapshot(context.Context) (model.Pure, error) { return model.Pure{}, nil }
// Technical closures owned entirely by Repo never cross the Model port.
func transaction(apply func() error) error { return apply() }
func persist() error { return transaction(func() error { return nil }) }
`)
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/value", Layer: "model", Module: "value", Owner: "RF05"},
		{Path: "internal/repo/value", Layer: "repo", Module: "value", Owner: "RF05"},
	}}
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	ports, violations := inspectPortGraph(root, graph, owners)
	if len(ports) != 4 {
		t.Fatalf("missing actual port methods: %+v", ports)
	}
	if len(violations) != 3 {
		t.Fatalf("expected callback AR03/AR04 and nested stream AR03 only: %+v", violations)
	}
	for _, violation := range violations {
		if strings.Contains(violation.Symbol, ".Snapshot") || strings.Contains(violation.Symbol, ".Content).Open") {
			t.Fatalf("pure snapshot or direct controlled stream rejected: %+v", violation)
		}
		if violation.Line <= 1 || len(violation.DependencyChain) < 2 {
			t.Fatalf("finding has no resolved source location/chain: %+v", violation)
		}
	}
	for _, port := range ports {
		if strings.Contains(port.Symbol, ".Repository).Commit") && len(port.Repositories) != 1 {
			t.Fatalf("implicit repository implementation was lost: %+v", port)
		}
	}
}
