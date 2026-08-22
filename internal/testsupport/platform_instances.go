// Package testsupport contains explicit database fixtures shared by integration tests.
package testsupport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"retrom/internal/platformcatalog"
	"retrom/internal/store"
)

var errLegacyDirectoryIdentityMissing = errors.New("testsupport: missing legacy directory identity")

// OpenDatabase opens a current-schema database and restores the historical directory fixture for tests whose
// subject is unrelated to recommended-directory initialization.
func OpenDatabase(ctx context.Context, path string, now func() time.Time) (*store.DB, error) {
	database, err := store.Open(ctx, path, now)
	if err != nil {
		return nil, fmt.Errorf("testsupport: open database: %w", err)
	}
	if err := SeedPlatformInstances(ctx, database.SQL); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	return database, nil
}

var legacyDirectoryIdentity = map[string]struct {
	ID   string
	Slug string
}{
	"nes/fceumm":                {"01980000-0000-7000-8000-000000000001", "nes-games"},
	"snes/snes9x":               {"01980000-0000-7000-8000-000000000003", "snes-games"},
	"gbc/gambatte":              {"01980000-0000-7000-8000-000000000004", "gbc-games"},
	"gba/mgba":                  {"01980000-0000-7000-8000-000000000005", "gba-games"},
	"arcade/fbneo":              {"01980000-0000-7000-8000-000000000006", "fbneo-games"},
	"arcade/mame2003_plus":      {"01980000-0000-7000-8000-000000000007", "mame2003-plus-games"},
	"arcade/fbalpha2012_cps1":   {"01980000-0000-7000-8000-000000000027", "fbalpha2012-cps1-games"},
	"arcade/fbalpha2012_cps2":   {"01980000-0000-7000-8000-000000000028", "fbalpha2012-cps2-games"},
	"dos/dosbox_pure":           {"01980000-0000-7000-8000-000000000009", "dos-games"},
	"nds/desmume2015":           {"01980000-0000-7000-8000-000000000010", "nds-games"},
	"atari2600/stella2014":      {"01980000-0000-7000-8000-000000000011", "atari-2600-games"},
	"atari5200/a5200":           {"01980000-0000-7000-8000-000000000012", "atari-5200-games"},
	"atari7800/prosystem":       {"01980000-0000-7000-8000-000000000013", "atari-7800-games"},
	"lynx/handy":                {"01980000-0000-7000-8000-000000000014", "atari-lynx-games"},
	"megadrive/genesis_plus_gx": {"01980000-0000-7000-8000-000000000015", "mega-drive-games"},
	"pce/mednafen_pce":          {"01980000-0000-7000-8000-000000000016", "pc-engine-games"},
	"ngpc/mednafen_ngp":         {"01980000-0000-7000-8000-000000000017", "neo-geo-pocket-games"},
	"n64/mupen64plus_next":      {"01980000-0000-7000-8000-000000000018", "nintendo-64-games"},
	"psx/pcsx_rearmed":          {"01980000-0000-7000-8000-000000000019", "playstation-games"},
	"saturn/yabause":            {"01980000-0000-7000-8000-000000000020", "sega-saturn-games"},
	"pcfx/mednafen_pcfx":        {"01980000-0000-7000-8000-000000000021", "pc-fx-games"},
	"3do/opera":                 {"01980000-0000-7000-8000-000000000022", "3do-games"},
	"psp/ppsspp":                {"01980000-0000-7000-8000-000000000023", "psp-games"},
	"virtualboy/beetle_vb":      {"01980000-0000-7000-8000-000000000024", "virtual-boy-games"},
	"wonderswan/mednafen_wswan": {"01980000-0000-7000-8000-000000000025", "wonderswan-games"},
	"mastersystem/smsplus":      {"01980000-0000-7000-8000-000000000026", "master-system-games"},
	"nintendo3ds/azahar":        {"01980000-0000-7000-8000-000000000029", "nintendo-3ds-games"},
}

// SeedPlatformInstances restores the former fixed directory rows only for tests
// whose subject is unrelated to recommended-directory initialization.
func SeedPlatformInstances(ctx context.Context, database *sql.DB) error {
	for _, template := range platformcatalog.Current().Templates {
		identity, exists := legacyDirectoryIdentity[template.Key]
		if !exists {
			return fmt.Errorf("%w: %s", errLegacyDirectoryIdentityMissing, template.Key)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO platform_instances(
  id,platform_id,default_core_id,name,slug,description,sort_order,enabled,version,
  created_at_ms,updated_at_ms,catalog_template_key
) VALUES(?,?,?,?,?,?,?,1,1,0,0,NULL)
`, identity.ID, template.PlatformID, template.DefaultCoreID, template.Name, identity.Slug,
			template.Description, template.CatalogOrder); err != nil {
			return fmt.Errorf("testsupport: seed platform instance %s: %w", template.Key, err)
		}
	}
	return nil
}
