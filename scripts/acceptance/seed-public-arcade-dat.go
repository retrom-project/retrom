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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"retrom/internal/arcadedat"
	"retrom/internal/cleanup"
	"retrom/internal/datindex"
)

var errUnsupportedSmokeCore = errors.New("unsupported smoke core")

func main() {
	if len(os.Args) != 4 {
		fatalf("usage: seed-public-arcade-dat <database> <mame2003|fbneo> <dat-file>")
	}
	if err := run(context.Background(), os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fatalf("seed public Arcade DAT: %v", err)
	}
}

func run(ctx context.Context, databasePath, coreID, datPath string) error {
	catalog, digestHex, err := loadSmokeCatalog(ctx, coreID, datPath)
	if err != nil {
		return err
	}
	database, err := openSmokeDatabase(ctx, databasePath)
	if err != nil {
		return err
	}
	defer func() { cleanup.Error("close acceptance database", database.Close()) }()
	artifactID, datID, err := installSmokeDAT(ctx, database, coreID, datPath, digestHex, catalog)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"artifactId":   artifactID,
		"coreId":       coreID,
		"datVersionId": datID,
		"sha256":       digestHex,
		"source":       "BUILTIN",
		"testOnly":     true,
	}); err != nil {
		return fmt.Errorf("encode acceptance DAT result: %w", err)
	}
	return nil
}

func loadSmokeCatalog(ctx context.Context, coreID, datPath string) (arcadedat.Catalog, string, error) {
	if !supportedSmokeCore(coreID) {
		return arcadedat.Catalog{}, "", fmt.Errorf("%w: %q", errUnsupportedSmokeCore, coreID)
	}

	contents, err := os.ReadFile(datPath)
	if err != nil {
		return arcadedat.Catalog{}, "", fmt.Errorf("read DAT: %w", err)
	}
	catalog, err := arcadedat.ParseCatalog(ctx, bytes.NewReader(contents), coreID)
	if err != nil {
		return arcadedat.Catalog{}, "", fmt.Errorf("parse DAT: %w", err)
	}
	digest := sha256.Sum256(contents)
	return catalog, hex.EncodeToString(digest[:]), nil
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
INSERT INTO dat_versions(id,core_id,core_artifact_id,source,builtin_relative_path,blob_id,sha256,
parser_version,compatibility_status,parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,
bios_set_count,default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,
unresolved_relation_count,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES(?,?,?,'BUILTIN',?,NULL,?,'retrom-dat-v1','MATCHED','READY',0,?,?,?,?,?,?,?,?,1,?,?,?,NULL)
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

func supportedSmokeCore(coreID string) bool {
	return coreID == "mame2003" || coreID == "fbneo"
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
