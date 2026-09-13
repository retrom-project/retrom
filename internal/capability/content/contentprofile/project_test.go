package contentprofile

import (
	"slices"
	"testing"
)

func TestProjectExtensionsFollowDeclaredArchiveFormats(t *testing.T) {
	previous := registry["rpgmaker"]
	t.Cleanup(func() { registry["rpgmaker"] = previous })
	changed := previous
	changed.ArchiveFormats = []ArchiveFormat{ArchiveSevenZip}
	registry["rpgmaker"] = changed
	if got := SupportedExtensions("rpgmaker"); !slices.Equal(got, []string{".7z"}) {
		t.Fatalf("project extensions drifted from its profile: %v", got)
	}
}

func TestProjectClassificationFollowsExistingProfiles(t *testing.T) {
	t.Parallel()
	for platformID, profile := range registry {
		kind, project := ProjectKind(platformID)
		if project != (profile.ArchivePolicy == ArchiveProject) {
			t.Fatalf("project classification drifted for %s", platformID)
		}
		if project && (string(kind) != profile.FormatCode || !IsProjectContentKind(kind)) {
			t.Fatalf("project kind/format drifted for %s", platformID)
		}
	}
	if _, project := ProjectKind("unknown"); project || IsProjectContentKind("UNKNOWN_PROJECT") ||
		IsProjectContentKind(ContentKindSingleFile) {
		t.Fatal("unknown or single-file content was admitted as a project")
	}
}
