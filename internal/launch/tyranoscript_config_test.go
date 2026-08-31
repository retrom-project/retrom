package launch

import "testing"

const validTyranoScriptCompatibility = `{"adapterAbi":"tyranoscript-snapshot-v1","bridgePath":"tyranoscript-bridge.js","gameCompatibilityLine":"tyranoscript-v1","readableSaveAbis":["tyranoscript-snapshot-v1"],"saveAbi":"tyranoscript-snapshot-v1"}`

func TestParseTyranoScriptCompatibility(t *testing.T) {
	t.Parallel()
	value, err := parseTyranoScriptCompatibility(validTyranoScriptCompatibility)
	if err != nil || value.BridgePath != "tyranoscript-bridge.js" ||
		value.GameCompatibilityLine != "tyranoscript-v1" {
		t.Fatalf("value=%#v error=%v", value, err)
	}
	for _, invalid := range []string{
		`{}`,
		`{"adapterAbi":"tyranoscript-snapshot-v0","bridgePath":"tyranoscript-bridge.js","gameCompatibilityLine":"tyranoscript-v1","readableSaveAbis":["tyranoscript-snapshot-v0"],"saveAbi":"tyranoscript-snapshot-v0"}`,
		`{"adapterAbi":"tyranoscript-snapshot-v1","bridgePath":"other.js","gameCompatibilityLine":"tyranoscript-v1","readableSaveAbis":["tyranoscript-snapshot-v1"],"saveAbi":"tyranoscript-snapshot-v1"}`,
		`{"adapterAbi":"tyranoscript-snapshot-v1","bridgePath":"tyranoscript-bridge.js","gameCompatibilityLine":"tyranoscript-v1","readableSaveAbis":["old-v1"],"saveAbi":"tyranoscript-snapshot-v1"}`,
		validTyranoScriptCompatibility[:len(validTyranoScriptCompatibility)-1] + `,"extra":true}`,
	} {
		if _, err := parseTyranoScriptCompatibility(invalid); err == nil {
			t.Fatalf("accepted invalid compatibility %s", invalid)
		}
	}
}
