package dependencies

import (
	"path/filepath"
	"testing"
)

func TestProductionLoadPinsMAMECurrentDAT(t *testing.T) {
	root := filepath.Join("..", "..", "data")
	set, err := LoadProduction(root, []string{"4.2.3", "4.3.0-pre"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if set.MAME == nil || len(set.MAME.Manifest.Cores) != 1 ||
		set.MAME.Manifest.Cores[0].CoreID != "mame_arcade" ||
		set.MAME.Manifest.Cores[0].ParseStats.MachineCount != 10049 {
		t.Fatal("published MAME Current DAT was not selected")
	}
}
