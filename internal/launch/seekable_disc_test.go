package launch

import (
	"strings"
	"testing"
)

func TestSeekableDiscUsesFrozenBlobIdentityAndRange(t *testing.T) {
	source := providerConfigSource{
		coreID: "play", providerID: "retrom-runtime", targetID: "play-ps2",
		bundleDigest: strings.Repeat("b", 64), contentKind: "SINGLE_FILE",
	}
	files := []lockedProviderFile{{
		logicalName: "disc.chd", format: "SOURCE_V1",
		digest: strings.Repeat("a", 64), size: 8_000_000_000,
	}}
	resource, err := providerBlobResource(source, "SEEKABLE_BLOB", files)
	if err != nil {
		t.Fatal(err)
	}
	if resource["rangeRequired"] != true || resource["sizeBytes"] != int64(8_000_000_000) ||
		resource["sha256"] != files[0].digest {
		t.Fatalf("incorrect seekable disc: %#v", resource)
	}
	url, ok := resource["url"].(string)
	if !ok || !strings.HasPrefix(url, RuntimeContentPath+"game/") || !strings.HasSuffix(url, "/disc.chd") {
		t.Fatalf("disc bypassed immutable content route: %v", resource["url"])
	}
}
