package launch

import (
	model "retrom/internal/model/launch"
	"slices"
	"testing"
)

func TestUploadedBIOSMissingEntriesRemainVisibleAsRuntimeWarnings(t *testing.T) {
	t.Parallel()
	for _, snapshot := range []string{
		`{"bios":[{"installationStatus":"MISSING_ENTRY"}]}`,
		`{"kind":"ARCADE","warnings":["neogeo.zip:MISSING_ENTRY"]}`,
	} {
		warnings := providerWarnings(model.ConfigSource{DependencyJSON: snapshot})
		if !slices.Contains(warnings, "BIOS_MISSING_ENTRY_WARNING") {
			t.Fatalf("missing BIOS advisory: %v", warnings)
		}
	}
}
