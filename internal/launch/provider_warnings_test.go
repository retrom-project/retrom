package launch

import (
	"slices"
	"testing"
)

func TestUploadedBIOSMissingEntriesRemainVisibleAsRuntimeWarnings(t *testing.T) {
	t.Parallel()
	for _, snapshot := range []string{
		`{"bios":[{"installationStatus":"MISSING_ENTRY"}]}`,
		`{"kind":"ARCADE","warnings":["neogeo.zip:MISSING_ENTRY"]}`,
	} {
		warnings := providerWarnings(providerConfigSource{dependencyJSON: snapshot})
		if !slices.Contains(warnings, "BIOS_MISSING_ENTRY_WARNING") {
			t.Fatalf("missing BIOS advisory: %v", warnings)
		}
	}
}
