package architecture

import (
	"slices"
	"strings"
	"testing"
)

func TestReexportsResolveAliasesValuesWrappersAndConsumers(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writeInventoryFile(t, root, "go.mod", "module retrom\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "internal/model/contracts/value.go", `package contracts
import "errors"
type Value struct { ID string }
var ErrAbsent = errors.New("absent")
const Limit = 7
func Same(v Value) Value { return v }
func Consume(v ...Value) int { return len(v) }
type Reader interface { Read() Value }
`)
	writeInventoryFile(t, root, "internal/service/domain/service.go", `package domain
import m "retrom/internal/model/contracts"
type Local = m.Value
type Exported = Local
type Batch = []*Local
var hidden = m.ErrAbsent
var ErrForwarded = hidden
var ForwardFunction = m.Same
const Limit = m.Limit
func Empty(v m.Value) m.Value { return m.Same(v) }
func Indirect(v m.Value) m.Value { return Empty(v) }
func Variadic(v ...m.Value) int { return m.Consume(v...) }
func Error() error { return ErrForwarded }
// A method call on an injected port is legitimate orchestration, not a reexport.
func Read(port m.Reader) m.Value { return port.Read() }
func Changed(v m.Value) m.Value { v.ID = "changed"; return m.Same(v) }
type Service struct{}
func (*Service) Execute(v m.Value) m.Value { return m.Same(v) }
`)
	writeInventoryFile(t, root, "internal/transport/use/use.go", `package use
import s "retrom/internal/service/domain"
var Value = s.Exported{}
var Failure = s.ErrForwarded
var Result = s.Empty(Value)
`)
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/contracts", Layer: "model", Module: "domain", Owner: "RF03"},
		{Path: "internal/service/domain", Layer: "service", Module: "domain", Owner: "RF03"},
		{Path: "internal/transport/use", Layer: "transport", Module: "http", Owner: "RF06"},
	}}
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	records, violations := inspectReexports(root, graph, owners)
	expected := []string{
		"Local", "Exported", "Batch", "hidden", "ErrForwarded", "ForwardFunction", "Limit",
		"Empty", "Indirect", "Variadic", "Error",
	}
	if len(records) != len(expected) || len(violations) != len(expected) {
		t.Fatalf("missing reexports or false positive: records=%+v violations=%+v", records, violations)
	}
	for _, record := range records {
		if !slices.Contains(expected, strings.TrimPrefix(record.Symbol, "retrom/internal/service/domain.")) ||
			record.Line < 3 || len(record.Targets) == 0 {
			t.Fatalf("incorrect declaration: %+v", record)
		}
		for _, target := range record.Targets {
			if !strings.HasPrefix(target, "retrom/internal/model/contracts.") {
				t.Fatalf("unresolved forwarding chain: %+v", record)
			}
		}
	}
	inventory := make([]GoPackageInventory, 0, len(graph))
	for _, pkg := range graph {
		inventory = append(inventory, GoPackageInventory{References: inventoryReferences(root, pkg)})
	}
	attachReexportConsumers(records, inventory)
	for _, record := range records {
		if strings.HasSuffix(record.Symbol, ".Exported") {
			if len(record.Consumers) != 1 || record.Consumers[0].File != "internal/transport/use/use.go" {
				t.Fatalf("actual alias consumer lost: %+v", record)
			}
		}
	}
}
