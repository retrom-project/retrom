package netplay

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// This literal frame is independent of the current header encoder and constants.
func TestNetplayStateFrameCompatibilityGolden(t *testing.T) {
	t.Parallel()
	frame, err := hex.DecodeString("524e533101980000000070008000000000000001019800000000700080000000000000020000000700000000000004d2000000050102030405")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseStateFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SessionID.String() != "01980000-0000-7000-8000-000000000001" ||
		parsed.Transfer.String() != "01980000-0000-7000-8000-000000000002" ||
		parsed.Epoch != 7 || parsed.NextFrame != 1234 || !bytes.Equal(parsed.Payload, []byte{1, 2, 3, 4, 5}) {
		t.Fatalf("state wire changed: %+v", parsed)
	}
	full, core, err := StateDigests(parsed.Payload)
	const want = "74f81fe167d99b4cb41d6d0ccda82278caee9f3e2f25d5e5a3936ff3dcec60d0"
	if err != nil || full != want || core != want {
		t.Fatalf("opaque payload digests = %s / %s / %v", full, core, err)
	}
	if WebSocketSubprotocol != "retrom.netplay.v1" || StateHeaderBytes != 52 {
		t.Fatalf("WebSocket contract = %s / %d", WebSocketSubprotocol, StateHeaderBytes)
	}
}
