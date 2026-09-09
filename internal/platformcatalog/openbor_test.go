package platformcatalog

import "testing"

func TestCatalogRecommendsOpenBOR(t *testing.T) {
	t.Parallel()
	for _, template := range Current().Templates {
		if template.Key == "openbor/openbor" && template.PlatformID == "openbor" && template.DefaultCoreID == "openbor" && template.Name == "OpenBOR 游戏" {
			return
		}
	}
	t.Fatal("OpenBOR recommendation missing")
}
