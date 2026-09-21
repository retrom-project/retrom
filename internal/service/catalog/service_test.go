package catalog

import (
	"context"
	"errors"
	"testing"

	"retrom/internal/contentcapability"
)

type repositoryStub struct {
	platforms []PlatformRow
	targets   []RuntimeTarget
	instances []PlatformInstance
	err       error
}

func (stub repositoryStub) Platforms(context.Context) ([]PlatformRow, error) {
	return stub.platforms, stub.err
}

func (stub repositoryStub) RuntimeTargets(context.Context) ([]RuntimeTarget, error) {
	return stub.targets, stub.err
}

func (stub repositoryStub) PlatformInstances(context.Context, PlatformInstanceQuery) ([]PlatformInstance, error) {
	return stub.instances, stub.err
}

type supportStub struct{ supported bool }

func (stub supportStub) SupportsPlatformTarget(string, string, string, string) bool {
	return stub.supported
}

func TestPlatformsAggregateDuplicateBindingsAndPreserveNetplaySupport(t *testing.T) {
	coreID, coreName, providerID, targetID := "core", "Core", "provider", "target"
	service := New(repositoryStub{platforms: []PlatformRow{
		{
			ID: "platform", Name: "Platform", SortOrder: 1, Enabled: true,
			CoreID: &coreID, CoreName: &coreName, CoreEnabled: boolPointer(true),
			ProviderID: &providerID, TargetID: &targetID,
		},
		{
			ID: "platform", Name: "Platform", SortOrder: 1, Enabled: true,
			CoreID: &coreID, CoreName: &coreName, CoreEnabled: boolPointer(true),
		},
	}}, supportStub{supported: true})
	items, err := service.Platforms(t.Context())
	if err != nil || len(items) != 1 || len(items[0].Cores) != 1 || !items[0].Cores[0].NetplaySupported {
		t.Fatalf("platform projection=%+v err=%v", items, err)
	}
}

func TestCatalogReadsWrapRepositoryErrors(t *testing.T) {
	cause := errors.New("catalog unavailable")
	service := New(repositoryStub{err: cause}, nil)
	if _, err := service.RuntimeTargets(t.Context()); !errors.Is(err, cause) {
		t.Fatalf("runtime target error lost cause: %v", err)
	}
}

func TestPlatformInstancesResolveDomainCapabilities(t *testing.T) {
	service := New(repositoryStub{instances: []PlatformInstance{{
		PlatformID: "saturn", Enabled: true,
		ContentPolicy: contentcapability.NewPolicy(contentcapability.ModeMultiDisc),
	}}}, nil)
	items, err := service.PlatformInstances(t.Context(), PlatformInstanceQuery{}, true)
	if err != nil || len(items) != 1 {
		t.Fatalf("platform instance projection=%+v err=%v", items, err)
	}
	if len(items[0].ImportCapabilities.ContentModes) != 2 || items[0].ImportCapabilities.MultiDisc == nil {
		t.Fatalf("capabilities=%+v", items[0].ImportCapabilities)
	}
}

func boolPointer(value bool) *bool { return &value }
