package launch

import (
	runtimebundle "retrom/internal/runtime/bundle"
)

const previewTestID = "01a00000-0000-7000-8000-000000000001"

type previewTestProvider struct {
	target runtimebundle.Target
	absent bool
	before func()
}

func (provider *previewTestProvider) Target(string, string) (runtimebundle.Target, bool) {
	if provider.before != nil {
		provider.before()
	}
	return provider.target, !provider.absent
}
func (*previewTestProvider) BundleSHA256(string, string) (string, bool) { return "bundle", true }
func previewContentFixture() PreviewSnapshot {
	dat := "dat"
	return PreviewSnapshot{Source: PreviewSource{
		SourceSnapshotID: "source", PlatformInstanceID: "instance", ProviderID: "provider", TargetID: "target", BundleSHA256: "bundle",
		CoreID: "core", DeliveryProfile: "ROM_BLOB", ContentKind: "SINGLE_FILE", DATVersionID: &dat, ValidationStatus: "READY", DependencySnapshot: "frozen",
	}, SourceFiles: []PreviewFile{{Role: "CONTENT", LogicalName: "game.bin", FileRecord: "game"}}}
}
