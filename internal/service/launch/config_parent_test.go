package launch

import (
	"context"
	"errors"
	"strings"
	"testing"

	runtimebundle "retrom/internal/runtime/bundle"
)

func TestParentConfigRequiresActualArchiveMetadata(t *testing.T) {
	t.Parallel()
	issuer, repository, builder := configTestFixture()
	builder.inputs = []runtimebundle.Input{{Role: "parent", Kind: "PARENT_ARCHIVE", Optional: true}}
	repository.snapshot.Files = []ConfigFile{{Role: "PARENT", LogicalName: "parent.zip", Digest: strings.Repeat("a", 64), Size: 100}}
	configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
	assertConfigRejected(t, configuration, err, ErrBlocked)
	if repository.activations != 0 || !errors.Is(err, ErrBlocked) {
		t.Fatal("parent with no archive metadata activated the launch")
	}
}

func TestParentConfigDescribesAuthorizedBytesAndPreservesContentURL(t *testing.T) {
	t.Parallel()
	issuer, repository, builder := configTestFixture()
	builder.inputs = []runtimebundle.Input{{Role: "parent", Kind: "PARENT_ARCHIVE", Optional: true}}
	file := ConfigFile{Role: "PARENT", LogicalName: "parent.zip", FileRecord: "frozen-record", Digest: strings.Repeat("a", 64), Size: 100}
	repository.snapshot.Files = []ConfigFile{file}
	calls := 0
	archive := ConfigArchive{SHA256: strings.Repeat("b", 64), SizeBytes: 234}
	issuer.environment.DescribeBundle = func(_ context.Context, files []ConfigFile) (ConfigArchive, error) {
		calls++
		if len(files) != 1 || files[0] != file {
			t.Fatal("did not describe frozen parent members")
		}
		return archive, nil
	}
	if _, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "invalid"); !errors.Is(err, ErrCredential) || calls != 0 {
		t.Fatal("unauthorized request read parent bytes")
	}
	if _, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid"); err != nil {
		t.Fatal(err)
	}
	resource := builder.input.Resources[0]
	identity, err := providerBundleIdentity([]ConfigFile{file})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || resource["sha256"] != archive.SHA256 || resource["sizeBytes"] != archive.SizeBytes ||
		resource["url"] != "/runtime/content/parent/"+identity+"/bundle.zip" {
		t.Fatalf("parent metadata = %#v", resource)
	}
}

func TestParentConfigReadFailureDoesNotActivate(t *testing.T) {
	t.Parallel()
	issuer, repository, builder := configTestFixture()
	builder.inputs = []runtimebundle.Input{{Role: "parent", Kind: "PARENT_ARCHIVE", Optional: true}}
	repository.snapshot.Files = []ConfigFile{{Role: "PARENT", LogicalName: "parent.zip", Digest: strings.Repeat("a", 64)}}
	issuer.environment.DescribeBundle = func(context.Context, []ConfigFile) (ConfigArchive, error) { return ConfigArchive{}, context.Canceled }
	configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
	assertConfigRejected(t, configuration, err, context.Canceled)
	if repository.transactions != 0 {
		t.Fatal("failed parent read entered activation")
	}
}
