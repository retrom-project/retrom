package launch

import (
	"context"
	"errors"
	"strings"
	"testing"

	model "retrom/internal/model/launch"

	"retrom/internal/capability/runtime/runtimebundle"
)

func TestOptionalConfigInputPreservesCanceledRead(t *testing.T) {
	t.Parallel()
	issuer, repository, builder := configTestFixture()
	builder.inputs = []runtimebundle.Input{{Role: "bios", Kind: "BIOS_BUNDLE", Optional: true}}
	repository.loadErr = context.Canceled
	configuration, err := issuer.Issue(t.Context(), model.SessionRef{ID: "launch"}, "valid")
	assertConfigRejected(t, configuration, err, context.Canceled)
	if repository.transactions != 0 {
		t.Fatal("canceled optional resource load activated a session")
	}
}

func TestOptionalConfigInputSkipsOnlyAbsentResource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, role, kind string
		files            []model.ConfigFile
		valid            bool
	}{
		{"absent bios", "bios", "BIOS_BUNDLE", nil, true},
		{"absent parent", "parent", "PARENT_ARCHIVE", nil, true},
		{"absent external", "external", "EXTERNAL_FILE_SET", nil, true},
		{"unknown role", "unknown", "BIOS_BUNDLE", nil, false},
		{"wrong kind", "bios", "ROM_BLOB", nil, false},
		{
			"invalid bios digest",
			"bios",
			"BIOS_BUNDLE",
			[]model.ConfigFile{{Role: "BIOS_BUNDLE", LogicalName: "bios.bin", Digest: "invalid"}},
			false,
		},
		{"ambiguous bios", "bios", "BIOS_BUNDLE", []model.ConfigFile{
			{Role: "BIOS_BUNDLE", LogicalName: "bios.bin", Digest: strings.Repeat("a", 64)},
			{Role: "BIOS_BUNDLE", LogicalName: "bios.bin", Digest: strings.Repeat("b", 64)},
		}, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			resources, err := providerResources(model.ConfigSnapshot{Files: item.files}, runtimebundle.Target{
				Inputs: []runtimebundle.Input{{Role: item.role, Kind: item.kind, Optional: true}},
			}, model.IsolationTicket{})
			if item.valid && (err != nil || len(resources) != 0) {
				t.Fatalf("absent optional: count=%d error=%v", len(resources), err)
			}
			if !item.valid && err == nil {
				t.Fatal("invalid optional resource silently omitted")
			}
		})
	}
}

func TestConfigRestoreRequiresReadableFrozenPayload(t *testing.T) {
	t.Parallel()
	target := runtimebundle.Target{}
	if _, _, err := providerRestore("launch", model.ConfigRestore{Required: true}, target); !errors.Is(err, model.ErrCredential) {
		t.Fatalf("missing requested restore: %v", err)
	}
	if _, _, err := providerRestore(
		"launch",
		model.ConfigRestore{Required: true, Found: true, Format: "unsupported"},
		target,
	); !errors.Is(
		err,
		model.ErrCredential,
	) {
		t.Fatalf("unsupported requested restore: %v", err)
	}
	if restore, found, err := providerRestore("launch", model.ConfigRestore{}, target); err != nil || found || restore != nil {
		t.Fatalf("absent restore: %v %v", restore, err)
	}
}
