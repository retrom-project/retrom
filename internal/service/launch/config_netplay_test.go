package launch

import "testing"

func TestNetplayEnvelopeUsesAbsolutePublicWebSocketURL(t *testing.T) {
	roomID := "01980000-0000-7000-8000-000000000001"
	sessionID := "01980000-0000-7000-8000-000000000002"
	player, profile := int64(1), `{}`
	source := ConfigSource{
		NetplayID:      &sessionID,
		NetplayRoom:    &roomID,
		NetplayProfile: &profile,
		NetplayPlayer:  &player,
	}
	for origin, expected := range map[string]string{
		"http://retrom.example:4000": "ws://retrom.example:4000/runtime/netplay/rooms/" + roomID + "/socket",
		"https://retrom.example:443": "wss://retrom.example:443/runtime/netplay/rooms/" + roomID + "/socket",
	} {
		value, mode, err := providerNetplay(origin, source)
		if err != nil || mode != "NETPLAY" {
			t.Fatalf("providerNetplay(%q) mode=%q error=%v", origin, mode, err)
		}
		netplay, ok := value.(map[string]any)
		if !ok || netplay["socketUrl"] != expected {
			t.Fatalf("providerNetplay(%q) = %#v, want socketUrl %q", origin, value, expected)
		}
	}
}
