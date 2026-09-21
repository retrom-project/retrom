package runtimecatalog

import "testing"

func TestPackDeclarationUsesTheSameUnicodeIdentityAsInstallation(t *testing.T) {
	pack := AssetPackDefinition{
		ID: "extra-assets", Kind: "ADDITIONAL_ASSETS", Generation: "RPGXP", DeclaredName: "Ｓtraße",
		NormalizedDeclaredName: "strasse", DisplayName: "Extra assets", RequiredLayoutVersion: "mkxpz-v1", Enabled: true,
	}
	if err := validatePackDefinitions([]AssetPackDefinition{pack}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*AssetPackDefinition){
		func(value *AssetPackDefinition) { value.RequiredLayoutVersion = "unknown-layout" },
		func(value *AssetPackDefinition) { value.Generation = "RPGMZ" },
		func(value *AssetPackDefinition) { value.Kind = "contains-hyphen" },
	} {
		invalid := pack
		change(&invalid)
		if err := validatePackDefinitions([]AssetPackDefinition{invalid}); err == nil {
			t.Fatalf("invalid declaration accepted: %#v", invalid)
		}
	}
}
