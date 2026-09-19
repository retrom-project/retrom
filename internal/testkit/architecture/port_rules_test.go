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
type VariadicStream interface { Read(...byte) (int, error) }
type BadVariadicContent interface { Open(context.Context) (VariadicStream, error) }
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
	writeInventoryFile(t, root, "internal/service/value/service.go", `package value
import ("context"; model "retrom/internal/model/value")
func Execute(ctx context.Context, port model.Repository) error { return port.Commit(ctx, model.Hidden{}) }
`)
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/value", Layer: "model", Module: "value", Owner: "RF05"},
		{Path: "internal/repo/value", Layer: "repo", Module: "value", Owner: "RF05"},
		{Path: "internal/service/value", Layer: "service", Module: "value", Owner: "RF05"},
	}}
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	ports, violations := inspectPortGraph(root, graph, owners)
	attachPortConsumers(ports, inspectFunctionGraph(root, graph, owners))
	if len(ports) != 6 {
		t.Fatalf("missing actual port methods: %+v", ports)
	}
	if len(violations) != 4 {
		t.Fatalf("expected callback AR03/AR04 and nested/variadic stream AR03 only: %+v", violations)
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
		if strings.Contains(port.Symbol, ".Repository).Commit") &&
			(len(port.Repositories) != 1 || len(port.RepositoryMethods) != 1 || len(port.Consumers) != 1) {
			t.Fatalf("implicit repository implementation was lost: %+v", port)
		}
	}
}

func TestDirectLeaseResultsHaveOneCloseOwner(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "internal/model/value/ports.go", `package value
type Lease interface { Close() error }
type Lock interface { Acquire(string) (Lease, error) }
type BadLeaseInput interface { Use(Lease) error }
type BadNestedLease interface { Open() (struct { Lease Lease }, error) }
type BusinessLease interface { Close() error; Apply() error }
type BadBusinessLease interface { OpenBusiness() (BusinessLease, error) }
type Repository interface { Snapshot() (Lease, error) }
`)
	writeInventoryFile(t, root, "internal/repo/value/repo.go", `package value
import model "retrom/internal/model/value"
type Repository struct{}
func (*Repository) Snapshot() (model.Lease, error) { return nil, nil }
`)
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/value", Layer: "model", Module: "value", Owner: "RF05"},
		{Path: "internal/repo/value", Layer: "repo", Module: "value", Owner: "RF05"},
	}}
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	_, violations := inspectPortGraph(root, graph, owners)
	want := map[string]int{
		"BadLeaseInput).Use": 1, "BadNestedLease).Open": 1,
		"BadBusinessLease).OpenBusiness": 1, "Repository).Snapshot": 2,
	}
	if len(violations) != 5 {
		t.Fatalf("expected invalid inputs, nested handles, business methods and Repo results: %+v", violations)
	}
	for _, violation := range violations {
		matched := false
		for symbol := range want {
			if strings.HasSuffix(violation.Symbol, symbol) {
				want[symbol]--
				matched = true
			}
		}
		if !matched {
			t.Fatalf("direct acquired lease was rejected: %+v", violation)
		}
	}
	for symbol, remaining := range want {
		if remaining != 0 {
			t.Fatalf("missing or duplicate findings for %s: remaining=%d", symbol, remaining)
		}
	}
}
