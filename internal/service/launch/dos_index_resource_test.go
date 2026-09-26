package launch

import (
	"strings"
	"testing"
)

func TestDOSGameUsesProjectedIndexResource(t *testing.T) {
	file := ConfigFile{
		Role: "GAME", LogicalName: "game.zip", Format: "RETROM_DOS_DIRECT_ZIP_V1",
		Digest: strings.Repeat("a", 64), Size: 1024,
	}
	source := ConfigSource{
		ContentKind: "DOS_BUNDLE", CoreID: "dosbox_pure", ProviderID: "emulatorjs",
		TargetID: "dosbox-pure", BundleDigest: strings.Repeat("b", 64),
	}
	resource, err := providerGameResource(source, "FILE_TREE", []ConfigFile{file}, IsolationTicket{})
	identity, identityErr := ContentIdentity(ContentView{
		Digest: file.Digest, Format: file.Format,
		CoreID: source.CoreID, ProviderID: source.ProviderID, TargetID: source.TargetID,
		BundleSHA256: source.BundleDigest,
	})
	if err != nil || identityErr != nil {
		t.Fatalf("DOS index resource: %v, identity: %v", err, identityErr)
	}
	url, urlErr := RuntimeContentURL("game", identity, "index.json")
	if urlErr != nil || resource["kind"] != "FILE_TREE" || resource["contentDigest"] != identity ||
		resource["indexUrl"] != url {
		t.Fatalf("DOS index resource = %#v, URL error = %v", resource, urlErr)
	}
}
