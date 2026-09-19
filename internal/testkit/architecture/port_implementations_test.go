package architecture

import (
	"slices"
	"testing"
)

func TestPortsIncludeServiceAndAdapterImplementations(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writePortImplementationFixture(t, root)
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/value", Layer: "model", Module: "value", Owner: "RF03"},
		{Path: "internal/service/provider", Layer: "service", Module: "provider", Owner: "RF03"},
		{Path: "internal/service/consumer", Layer: "service", Module: "consumer", Owner: "RF03"},
		{Path: "internal/adapter/content", Layer: "adapter", Module: "content", Owner: "RF04"},
	}}
	graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, "default")
	if err != nil {
		t.Fatal(err)
	}
	ports, violations := inspectPortGraph(root, graph, owners)
	attachPortConsumers(ports, inspectFunctionGraph(root, graph, owners))
	if len(violations) != 0 || len(ports) != 3 {
		t.Fatalf("unexpected ports or value violations: %+v / %+v", ports, violations)
	}
	workflow := findImplementationPort(t, ports, "(retrom/internal/model/value.Workflow).Cancel")
	if len(workflow.Repositories) != 0 || len(workflow.RepositoryMethods) != 0 {
		t.Fatalf("service confused with repository: %+v", workflow)
	}
	expected := []string{
		"*retrom/internal/service/provider.Flow",
		"*retrom/internal/service/provider.Promoted",
	}
	if !slices.Equal(workflow.Implementations, expected) ||
		!slices.Equal(workflow.ImplementationMethods, []string{"(*retrom/internal/service/provider.Flow).Cancel"}) {
		t.Fatalf("implicit or promoted service implementation lost: %+v", workflow)
	}
	if len(workflow.Consumers) != 2 {
		t.Fatalf("interface and concrete consumers must both be retained: %+v", workflow)
	}
	content := findImplementationPort(t, ports, "(retrom/internal/model/value.Content).Open")
	if !slices.Equal(content.Implementations, []string{"*retrom/internal/adapter/content.Source"}) ||
		!slices.Equal(content.ImplementationMethods, []string{"(retrom/internal/adapter/content.Source).Open"}) ||
		len(content.Consumers) != 1 {
		t.Fatalf("value-receiver adapter or consumer lost: %+v", content)
	}
}

func findImplementationPort(t *testing.T, ports []PortInventory, symbol string) PortInventory {
	t.Helper()
	for _, port := range ports {
		if port.Symbol == symbol {
			return port
		}
	}
	t.Fatalf("missing port %s", symbol)
	return PortInventory{}
}

func writePortImplementationFixture(t *testing.T, root string) {
	t.Helper()
	writeInventoryFile(t, root, "go.mod", "module retrom\n\ngo 1.26.5\n")
	writeInventoryFile(t, root, "internal/model/value/ports.go", `package value
import ("context"; "io")
type Workflow interface {
    Cancel(context.Context, string) error
    State(context.Context) (string, error)
}
type Content interface { Open(context.Context, string) (io.ReadCloser, error) }
`)
	writeInventoryFile(t, root, "internal/service/provider/flow.go", `package provider
import "context"
type Flow struct{}
func (*Flow) Cancel(context.Context, string) error { return nil }
func (*Flow) State(context.Context) (string, error) { return "", nil }
type Promoted struct { *Flow }
type Alias = Flow
type Partial struct{}
func (*Partial) Cancel(context.Context, string) error { return nil }
type Wrong struct{}
func (*Wrong) Cancel(context.Context, int) error { return nil }
func (*Wrong) State(context.Context) (string, error) { return "", nil }
`)
	writeInventoryFile(t, root, "internal/adapter/content/source.go", `package content
import ("context"; "io")
type Source struct{}
func (Source) Open(context.Context, string) (io.ReadCloser, error) { return nil, nil }
`)
	writeInventoryFile(t, root, "internal/service/consumer/consumer.go", `package consumer
import ("context"; model "retrom/internal/model/value"; "retrom/internal/service/provider")
func ThroughPort(ctx context.Context, port model.Workflow) error { return port.Cancel(ctx, "id") }
func Direct(ctx context.Context, flow *provider.Flow) error { return flow.Cancel(ctx, "id") }
func Open(ctx context.Context, port model.Content) error { _, err := port.Open(ctx, "id"); return err }
`)
}

func TestPortImplementationsRetainIntegrationBuild(t *testing.T) {
	t.Parallel()
	root := newInventoryRepository(t)
	writePortImplementationFixture(t, root)
	writeInventoryFile(t, root, "internal/adapter/content/source_integration.go", `//go:build integration

package content
import ("context"; "io")
type IntegrationSource struct{}
func (IntegrationSource) Open(context.Context, string) (io.ReadCloser, error) { return nil, nil }
`)
	owners := OwnershipRegistry{Packages: []PackageOwnership{
		{Path: "internal/model/value", Layer: "model", Module: "value", Owner: "RF03"},
		{Path: "internal/service/provider", Layer: "service", Module: "provider", Owner: "RF03"},
		{Path: "internal/service/consumer", Layer: "service", Module: "consumer", Owner: "RF03"},
		{Path: "internal/adapter/content", Layer: "adapter", Module: "content", Owner: "RF04"},
	}}
	var all []PortInventory
	for _, build := range []string{"default", "integration"} {
		graph, err := loadInventoryGraph(t.Context(), root, []string{"./internal/..."}, build)
		if err != nil {
			t.Fatal(err)
		}
		ports, violations := inspectPortGraph(root, graph, owners)
		if len(violations) != 0 {
			t.Fatalf("unexpected port violation: %+v", violations)
		}
		all = append(all, ports...)
	}
	merged := mergePortBuilds(all)
	if len(merged) != 3 {
		t.Fatalf("expected three unique interface methods, got %d", len(merged))
	}
	content := findImplementationPort(t, merged, "(retrom/internal/model/value.Content).Open")
	expected := []string{
		"*retrom/internal/adapter/content.IntegrationSource",
		"*retrom/internal/adapter/content.Source",
	}
	if !slices.Equal(content.Implementations, expected) || len(content.ImplementationMethods) != 2 {
		t.Fatalf("additional build implementation was discarded: %+v", content)
	}
}
