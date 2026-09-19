package firmwaremanifest

import "testing"

func TestPinnedCatalogDeclaresArchiveSlotsAndDefaultBIOS(t *testing.T) {
	catalog, err := (Source{}).LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Source.CoreID != "same_cdi" || len(catalog.Items) != 3 {
		t.Fatalf("unexpected catalog: %#v", catalog)
	}
	modes := map[string]string{"cdimono1.zip": "REQUIRED", "cdimono2.zip": "OPTIONAL", "cdibios.zip": "OPTIONAL"}
	counts := map[string]int{"cdimono1.zip": 3, "cdimono2.zip": 3, "cdibios.zip": 2}
	for _, item := range catalog.Items {
		if item.Mode != modes[item.LogicalName] || item.EmulatorPath != "/same_cdi/bios/"+item.LogicalName {
			t.Fatalf("unexpected firmware: %#v", item)
		}
		required := 0
		for _, member := range item.Members {
			if member.Required {
				required++
			}
		}
		if required != counts[item.LogicalName] {
			t.Errorf("%s required members=%d", item.LogicalName, required)
		}
		delete(modes, item.LogicalName)
	}
	if len(modes) != 0 {
		t.Fatalf("missing slots: %v", modes)
	}
}
