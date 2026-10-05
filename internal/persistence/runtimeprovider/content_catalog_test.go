package runtimeprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"retrom/internal/content/requirements"
	"retrom/internal/persistence/contentquery"
	runtimebundle "retrom/internal/runtime/bundle"
	runtimecatalog "retrom/internal/runtime/catalog"
	service "retrom/internal/service/runtimeprovider"
)

func TestActivationPreservesVerifiedCatalogOutsidePublicManifest(t *testing.T) {
	initial := projectionFixture("1.0.0", "a", []string{"state-v1"})
	provider := initial.Providers[0].Active
	target := initial.Providers[0].Targets[0].Target
	core := requirements.Asset{Path: "assets/core.data", SHA256: strings.Repeat("b", 64)}
	document, err := json.Marshal(requirements.FlycastCatalog{
		SchemaVersion: 1, Kind: "FLYCAST_ROM_REQUIREMENTS",
		Core: requirements.FlycastCore{
			Filename: "core.data", SHA256: core.SHA256,
			SourceCommit: strings.Repeat("a", 40), TableSHA256: core.SHA256, ExporterSHA256: core.SHA256,
		},
		Machines: []requirements.FlycastMachine{{
			Name: "cart", Platform: "naomi", MediaType: "CARTRIDGE",
			Files: []requirements.ROMFile{{Name: "game.bin", SizeBytes: 4}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(document)
	target.ContentRequirements = &requirements.Policy{
		Kind: requirements.FlycastCartridge, Platform: "naomi",
		Core: &core, Catalog: &requirements.Asset{Path: "assets/catalog.json", SHA256: hex.EncodeToString(digest[:])},
		CatalogJSON: document,
	}
	projection, err := service.NewProjection(runtimebundle.ActiveDescriptor{
		SchemaVersion: 1, Source: "candidate",
		Providers: []runtimebundle.ActiveProvider{provider},
	}, map[string]runtimebundle.Manifest{
		"fixture": {
			SchemaVersion: 2, ProviderID: "fixture", ProviderVersion: "1.0.0", ProviderAPI: 2,
			ClientModulePath: "client.mjs", Targets: []runtimebundle.Target{target},
		},
	}, runtimecatalog.Catalog{SchemaVersion: 1, Definitions: initial.Definitions, Bindings: initial.Bindings})
	if err != nil {
		t.Fatal(err)
	}
	projected := projection.Providers[0].Targets[0]
	if len(projected.RequirementCatalog) == 0 || strings.Contains(projected.ManifestFragment, "machines") {
		t.Fatal("catalog must survive projection without entering the public target fragment")
	}
	document[0] = 'x'
	db := openProjectionDatabase(t)
	if err := service.New(New(db.SQL)).Reconcile(t.Context(), projection, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	policy := projected.Target.ContentRequirements
	if err := contentquery.LoadRequirements(t.Context(), db.SQL, policy); err != nil {
		t.Fatal(err)
	}
	if policy.CatalogFacts == nil || policy.CatalogFacts.Machines[0].Name != "cart" {
		t.Fatal("installed catalog was lost or shared mutable input survived projection")
	}
}
