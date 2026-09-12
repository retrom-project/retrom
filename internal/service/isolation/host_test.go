package isolation

import (
	"strings"
	"testing"
	"time"
)

func TestResolveHostRequiresCanonicalUUIDAsCompleteLeftmostLabel(t *testing.T) {
	t.Parallel()
	service := New(nil, "http://{launchId}.rpg.feature-a1b2c3d4e5f6.localhost:3000", time.Now)
	launchID := "018fdb34-4f5d-7abc-8def-0123456789ab"
	access, ok := service.ResolveHost(launchID + ".rpg.feature-a1b2c3d4e5f6.localhost:3000")
	if !ok || access.LaunchID != launchID || access.Origin != "http://"+launchID+".rpg.feature-a1b2c3d4e5f6.localhost:3000" {
		t.Fatalf("resolved access = %#v, %t", access, ok)
	}
	for _, host := range []string{
		strings.ToUpper(launchID) + ".rpg.feature-a1b2c3d4e5f6.localhost:3000",
		"prefix." + launchID + ".rpg.feature-a1b2c3d4e5f6.localhost:3000",
		launchID + ".extra.rpg.feature-a1b2c3d4e5f6.localhost:3000",
		"not-a-uuid.rpg.feature-a1b2c3d4e5f6.localhost:3000",
		launchID + ".rpg.feature-a1b2c3d4e5f6.localhost:443",
	} {
		if _, accepted := service.ResolveHost(host); accepted {
			t.Fatalf("invalid runtime host accepted: %s", host)
		}
	}
}

func TestRuntimeHostCandidateFailsClosedWithoutClaimingApplicationHosts(t *testing.T) {
	t.Parallel()
	service := New(nil, "http://{launchId}.rpg.localhost:8080", time.Now)
	if !service.IsRuntimeHostCandidate("invalid.rpg.localhost:8080") {
		t.Fatal("invalid runtime-suffix host was not recognized as a candidate")
	}
	for _, host := range []string{"localhost:8080", "app.localhost:3000", "rpg.localhost:8080"} {
		if service.IsRuntimeHostCandidate(host) {
			t.Fatalf("application host claimed as runtime candidate: %s", host)
		}
	}
}
