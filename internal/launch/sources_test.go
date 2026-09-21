package launch

import (
	"errors"
	"testing"

	"retrom/internal/runtimelaunch"
)

func TestSourcesWithoutProviderRemainUnavailable(t *testing.T) {
	source := NewSources(nil, nil)
	if _, found := source.Target("provider", "target"); found {
		t.Fatal("unconfigured target found")
	}
	if _, found := source.BundleSHA256("provider", "target"); found {
		t.Fatal("unconfigured bundle found")
	}
	if _, found := source.AssetPaths("provider", "target"); found {
		t.Fatal("unconfigured assets found")
	}
	if _, err := source.Build(runtimelaunch.Input{}); !errors.Is(err, runtimelaunch.ErrEnvelopeInvalid) {
		t.Fatalf("unconfigured envelope: %v", err)
	}
}
