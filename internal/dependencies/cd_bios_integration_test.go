//go:build integration

package dependencies

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/testsupport"
)

func TestCDRequirementsDoNotBlockPCECartridges(t *testing.T) {
	ctx := context.Background()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	set, err := Load(filepath.Join("..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		target, name, status, blocker string
		count                         int
	}{
		{"mednafen-pce", "cartridge.pce", "READY", "READY", 0},
		{"mednafen-pce", "disc.chd", "BLOCKED", "LAUNCH_BIOS_MISSING", 1},
		{"same-cdi", "disc.chd", "BLOCKED", "LAUNCH_BIOS_MISSING", 3},
	} {
		t.Run(test.target+"/"+test.name, func(t *testing.T) {
			snapshot, status, blocker, err := corevalidation.ResolveBIOS(ctx, database.SQL, "emulatorjs", test.target, test.name)
			if err != nil || status != test.status || blocker != test.blocker || len(snapshot.BIOS) != test.count {
				t.Fatalf("BIOS resolution = %#v, %s, %s, %v", snapshot, status, blocker, err)
			}
			for _, bios := range snapshot.BIOS {
				if test.target == "same-cdi" && (bios.DeliveryKind != "EXTERNAL_FILE" || bios.EmulatorPath == nil ||
					!strings.HasPrefix(*bios.EmulatorPath, "/same_cdi/bios/cdimono1/")) {
					t.Errorf("CD-i BIOS must retain its native firmware path: %#v", bios)
				}
			}
		})
	}
}
