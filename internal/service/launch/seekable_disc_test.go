package launch

import (
	model "retrom/internal/model/launch"
	"strings"
	"testing"
)

func TestSeekableDiscUsesFrozenBlobIdentityAndRange(t *testing.T) {
	source := model.ConfigSource{
		CoreID: "play", ProviderID: "retrom-runtime", TargetID: "play-ps2",
		BundleDigest: strings.Repeat("b", 64), ContentKind: "SINGLE_FILE",
	}
	files := []model.ConfigFile{{
		LogicalName: "disc.chd", Format: "SOURCE_V1",
		Digest: strings.Repeat("a", 64), Size: 8_000_000_000,
	}}
	resource, err := providerBlobResource(source, "SEEKABLE_BLOB", files)
	if err != nil {
		t.Fatal(err)
	}
	if resource["rangeRequired"] != true || resource["sizeBytes"] != int64(8_000_000_000) ||
		resource["sha256"] != files[0].Digest {
		t.Fatalf("incorrect seekable disc: %#v", resource)
	}
	url, ok := resource["url"].(string)
	if !ok || !strings.HasPrefix(url, RuntimeContentPath+"game/") || !strings.HasSuffix(url, "/disc.chd") {
		t.Fatalf("disc bypassed immutable content route: %v", resource["url"])
	}
}
