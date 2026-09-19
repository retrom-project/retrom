package runtimecatalog

import (
	"testing"

	runtimecontract "retrom/internal/model/runtimecontract"
)

func TestPackDeclarationUsesTheSameUnicodeIdentityAsInstallation(t *testing.T) {
	pack := runtimecontract.AssetPackDefinition{
		ID: "extra-assets", Kind: "ADDITIONAL_ASSETS", Generation: "RPGXP", DeclaredName: "Ｓtraße",
		NormalizedDeclaredName: "strasse", DisplayName: "Extra assets", RequiredLayoutVersion: "mkxpz-v1", Enabled: true,
	}
	if err := validatePackDefinitions([]runtimecontract.AssetPackDefinition{pack}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*runtimecontract.AssetPackDefinition){
		func(value *runtimecontract.AssetPackDefinition) { value.RequiredLayoutVersion = "unknown-layout" },
		func(value *runtimecontract.AssetPackDefinition) { value.Generation = "RPGMZ" },
		func(value *runtimecontract.AssetPackDefinition) { value.Kind = "contains-hyphen" },
	} {
		invalid := pack
		change(&invalid)
		if err := validatePackDefinitions([]runtimecontract.AssetPackDefinition{invalid}); err == nil {
			t.Fatalf("invalid declaration accepted: %#v", invalid)
		}
	}
}
