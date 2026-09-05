package launch

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetplayEnvelopeUsesAbsolutePublicWebSocketURL(t *testing.T) {
	roomID := "01980000-0000-7000-8000-000000000001"
	sessionID := "01980000-0000-7000-8000-000000000002"
	source := providerConfigSource{
		netplayID:      sql.NullString{String: sessionID, Valid: true},
		netplayRoom:    sql.NullString{String: roomID, Valid: true},
		netplayProfile: sql.NullString{String: `{}`, Valid: true},
		netplayPlayer:  sql.NullInt64{Int64: 1, Valid: true},
	}
	for origin, expected := range map[string]string{
		"http://retrom.example:4000": "ws://retrom.example:4000/runtime/netplay/rooms/" + roomID + "/socket",
		"https://retrom.example:443": "wss://retrom.example:443/runtime/netplay/rooms/" + roomID + "/socket",
	} {
		value, mode, err := (&Service{publicOrigin: origin}).providerNetplay(source)
		if err != nil || mode != "NETPLAY" {
			t.Fatalf("providerNetplay(%q) mode=%q error=%v", origin, mode, err)
		}
		netplay, ok := value.(map[string]any)
		if !ok || netplay["socketUrl"] != expected {
			t.Fatalf("providerNetplay(%q) = %#v, want socketUrl %q", origin, value, expected)
		}
	}
}

func TestProductionLaunchHasOneProviderTargetEnvelopePath(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		contents, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		source.Write(contents)
	}
	for _, forbidden := range []string{
		"runtime_family", "runtimeFamily", "route_key", "routeKey", "core_artifacts",
		"core_artifact_id", "CoreArtifactID", "adapterAbi", "saveAbi", "payloadKind",
		"nativeProfile", "resumeSlot",
	} {
		if strings.Contains(source.String(), forbidden) {
			t.Fatalf("legacy launch authority %q remains in production source", forbidden)
		}
	}
	if count := strings.Count(source.String(), "runtimeBuilder.Build("); count != 1 {
		t.Fatalf("runtimeBuilder.Build call count = %d, want one generic Envelope path", count)
	}
}
