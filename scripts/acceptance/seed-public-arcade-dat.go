// Command seed-public-arcade-dat installs the project-owned smoke DAT as a
// test-only BUILTIN catalog in an already initialized acceptance database.
// It deliberately has no production HTTP/API equivalent: release DAT
// materialization and selection are covered separately by ACC-DAT-004.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"retrom/internal/arcadedat"
	"retrom/internal/cleanup"
	"retrom/internal/datindex"
)

var errUnsupportedSmokeFixture = errors.New("unsupported smoke fixture")

type smokeFixture struct {
	CoreID       string
	RelativePath string
	SHA256       string
	Machines     []string
}

var smokeFixtures = map[string]smokeFixture{
	"mame2003": {
		CoreID: "mame2003", RelativePath: "testdata/public-roms/arcade-smoke/mame2003-smoke.xml",
		SHA256:   "746f0828479bd8749596c0f57af43e5f46afca215f0bb3e005d53a2adb2994c8",
		Machines: []string{"retrombios", "puckman", "pacman"},
	},
	"fbneo": {
		CoreID: "fbneo", RelativePath: "testdata/public-roms/arcade-smoke/fbneo/fbneo-smoke.dat",
		SHA256:   "f460da0fd6d2f2613df3838dad956df05f453d023db04b112eba44ff4121341a",
		Machines: []string{"retrombios", "puckman", "pacman"},
	},
	"mame2003_plus": {
		CoreID: "mame2003_plus", RelativePath: "testdata/public-roms/arcade-smoke/mame2003_plus/mame2003-plus-smoke.xml",
		SHA256:   "746f0828479bd8749596c0f57af43e5f46afca215f0bb3e005d53a2adb2994c8",
		Machines: []string{"retrombios", "puckman", "pacman"},
	},
	"fbalpha2012_cps1": {
		CoreID: "fbalpha2012_cps1", RelativePath: "testdata/public-roms/arcade-smoke/fbalpha2012_cps1/fbalpha2012-cps1-smoke.dat",
		SHA256:   "9d1dfba059d6e9f5429dbe982c6a13ae5ba4b7ddbea053279392fd3da637d205",
		Machines: []string{"1941"},
	},
	"fbalpha2012_cps2": {
		CoreID: "fbalpha2012_cps2", RelativePath: "testdata/public-roms/arcade-smoke/fbalpha2012_cps2/fbalpha2012-cps2-smoke.dat",
		SHA256:   "121e3e16c7a604448392b5086f2c28293b98c830b6face5a00d778d311b439c4",
		Machines: []string{"spf2t", "spf2xjd"},
	},
}

func main() {
	arguments := flag.NewFlagSet("seed-public-arcade-dat", flag.ContinueOnError)
	fixtureID := arguments.String("fixture", "", "allowlisted public fixture ID")
	databasePath := arguments.String("database", "", "acceptance SQLite database")
	if err := arguments.Parse(os.Args[1:]); err != nil || arguments.NArg() != 0 || *fixtureID == "" || *databasePath == "" {
		fatalf("usage: seed-public-arcade-dat --database <database> --fixture <mame2003|fbneo|mame2003_plus|fbalpha2012_cps1|fbalpha2012_cps2>")
	}
	if err := run(context.Background(), *databasePath, *fixtureID); err != nil {
		fatalf("seed public Arcade DAT: %v", err)
	}
}

func run(ctx context.Context, databasePath, fixtureID string) error {
	fixture, ok := smokeFixtures[fixtureID]
	if !ok {
		return fmt.Errorf("%w: %q", errUnsupportedSmokeFixture, fixtureID)
	}
	catalog, digestHex, datPath, err := loadSmokeCatalog(ctx, fixture)
	if err != nil {
		return err
	}
	database, err := openSmokeDatabase(ctx, databasePath)
	if err != nil {
		return err
	}
	defer func() { cleanup.Error("close acceptance database", database.Close()) }()
	artifactID, datID, err := installSmokeDAT(ctx, database, fixture.CoreID, datPath, digestHex, catalog)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"artifactId":   artifactID,
		"coreId":       fixture.CoreID,
		"datVersionId": datID,
		"sha256":       digestHex,
		"source":       "BUILTIN",
		"testOnly":     true,
	}); err != nil {
		return fmt.Errorf("encode acceptance DAT result: %w", err)
	}
	return nil
}

func loadSmokeCatalog(
	ctx context.Context,
	fixture smokeFixture,
) (arcadedat.Catalog, string, string, error) {
	relativePath := filepath.Clean(fixture.RelativePath)
	if filepath.IsAbs(relativePath) || relativePath != fixture.RelativePath || !filepath.IsLocal(relativePath) {
		return arcadedat.Catalog{}, "", "", errors.New("unsafe smoke fixture path")
	}
	repositoryRoot, err := findRepositoryRoot()
	if err != nil {
		return arcadedat.Catalog{}, "", "", err
	}
	datPath := filepath.Join(repositoryRoot, relativePath)
	contents, err := os.ReadFile(datPath)
	if err != nil {
		return arcadedat.Catalog{}, "", "", fmt.Errorf("read DAT: %w", err)
	}
	digest := sha256.Sum256(contents)
	digestHex := hex.EncodeToString(digest[:])
	if digestHex != fixture.SHA256 {
		return arcadedat.Catalog{}, "", "", errors.New("smoke fixture DAT digest drift")
	}
	catalog, err := arcadedat.ParseCatalog(ctx, bytes.NewReader(contents), fixture.CoreID)
	if err != nil {
		return arcadedat.Catalog{}, "", "", fmt.Errorf("parse DAT: %w", err)
	}
	machines := make([]string, 0, len(catalog.Machines))
	for _, machine := range catalog.Machines {
		machines = append(machines, machine.Name)
	}
	if !slices.Equal(machines, fixture.Machines) {
		return arcadedat.Catalog{}, "", "", errors.New("smoke fixture machine set drift")
	}
	return catalog, digestHex, datPath, nil
}

func findRepositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve acceptance working directory: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(directory, "go.mod")); statErr == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("acceptance repository root not found")
		}
		directory = parent
	}
}

func openSmokeDatabase(ctx context.Context, databasePath string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		cleanup.Error("close acceptance database", database.Close())
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		cleanup.Error("close acceptance database", database.Close())
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	return database, nil
}

func installSmokeDAT(
	ctx context.Context,
	database *sql.DB,
	coreID, datPath, digestHex string,
	catalog arcadedat.Catalog,
) (string, string, error) {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var artifactID string
	if err := transaction.QueryRowContext(ctx, `
SELECT id FROM core_artifacts WHERE core_id=? AND enabled=1
`, coreID).Scan(&artifactID); err != nil {
		return "", "", fmt.Errorf("find enabled core artifact: %w", err)
	}
	datID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("retrom:acceptance:arcade-dat:"+artifactID+":"+digestHex)).String()
	nowMS := time.Now().UTC().UnixMilli()
	stats := catalog.Stats
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO dat_versions(id,core_id,core_artifact_id,builtin_relative_path,sha256,
parser_version,parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,
bios_set_count,default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,
unresolved_relation_count,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES(?,?,?,?,?,'retrom-dat-v1','READY',0,?,?,?,?,?,?,?,?,1,?,?,?,NULL)
`, datID, coreID, artifactID, "acceptance/"+coreID+"/"+filepath.Base(datPath), digestHex,
		stats.MachineCount, stats.ROMEntryCount, stats.DiskEntryCount, stats.BIOSSetCount,
		stats.DefaultBIOSSetCount, stats.ExplicitBIOSMachineCount, stats.BaseDependencyTargetCount,
		stats.UnresolvedCloneofTargetCount+stats.UnresolvedRomofTargetCount, nowMS, nowMS, nowMS); err != nil {
		return "", "", fmt.Errorf("insert test-only built-in DAT: %w", err)
	}
	if err := datindex.Replace(ctx, transaction, datID, catalog); err != nil {
		return "", "", fmt.Errorf("index test-only built-in DAT: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_versions SET is_active=0,version=version+1,updated_at_ms=?
WHERE core_artifact_id=? AND is_active=1
`, nowMS, artifactID); err != nil {
		return "", "", fmt.Errorf("deactivate release DAT for test fixture: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_versions SET is_active=1,activated_at_ms=?,updated_at_ms=?,version=version+1 WHERE id=?
`, nowMS, nowMS, datID); err != nil {
		return "", "", fmt.Errorf("activate test-only built-in DAT: %w", err)
	}
	if err := datindex.SyncRequirements(ctx, transaction, datID, time.UnixMilli(nowMS)); err != nil {
		return "", "", fmt.Errorf("sync test-only built-in DAT requirements: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET version=version+1,updated_at_ms=? WHERE id=?
`, nowMS, artifactID); err != nil {
		return "", "", fmt.Errorf("advance smoke core artifact: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return "", "", fmt.Errorf("commit test-only built-in DAT: %w", err)
	}
	return artifactID, datID, nil
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
