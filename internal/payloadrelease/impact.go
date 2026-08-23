package payloadrelease

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"retrom/internal/blobregistry"
	"retrom/internal/cleanup"
)

type GameImpact struct {
	ImpactDigest       string   `json:"impactDigest"`
	RegisteredBytes    string   `json:"registeredBytes"`
	ExclusiveBytes     string   `json:"exclusiveBytes"`
	SharedBytes        string   `json:"sharedBytes"`
	BlobCount          int64    `json:"blobCount"`
	SaveStateCount     int64    `json:"saveStateCount"`
	AssetCount         int64    `json:"assetCount"`
	ContentFileCount   int64    `json:"contentFileCount"`
	ActiveLaunchCount  int64    `json:"activeLaunchCount"`
	ActiveNetplayCount int64    `json:"activeNetplayCount"`
	ReviewEventCount   int64    `json:"reviewEventCount"`
	SourceKinds        []string `json:"sourceKinds"`
}

type impactCanonical struct {
	RegisteredBytes    string   `json:"registeredBytes"`
	ExclusiveBytes     string   `json:"exclusiveBytes"`
	SharedBytes        string   `json:"sharedBytes"`
	BlobCount          int64    `json:"blobCount"`
	SaveStateCount     int64    `json:"saveStateCount"`
	AssetCount         int64    `json:"assetCount"`
	ContentFileCount   int64    `json:"contentFileCount"`
	ActiveLaunchCount  int64    `json:"activeLaunchCount"`
	ActiveNetplayCount int64    `json:"activeNetplayCount"`
	ReviewEventCount   int64    `json:"reviewEventCount"`
	SourceKinds        []string `json:"sourceKinds"`
}

// GameDeleteAuditImpact intentionally omits ImpactDigest. The digest protects
// the delete precondition, but is not payload lifecycle audit evidence.
func GameDeleteAuditImpact(impact GameImpact) map[string]any {
	return map[string]any{
		"registeredBytes": impact.RegisteredBytes, "exclusiveBytes": impact.ExclusiveBytes,
		"sharedBytes": impact.SharedBytes, "blobCount": impact.BlobCount,
		"saveStateCount": impact.SaveStateCount, "assetCount": impact.AssetCount,
		"contentFileCount": impact.ContentFileCount, "activeLaunchCount": impact.ActiveLaunchCount,
		"activeNetplayCount": impact.ActiveNetplayCount, "reviewEventCount": impact.ReviewEventCount,
		"sourceKinds": impact.SourceKinds,
	}
}

func GameDeleteImpact(ctx context.Context, database *sql.DB, gameID string) (GameImpact, error) {
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return GameImpact{}, fmt.Errorf("payloadrelease/impact transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	result, err := GameDeleteImpactTx(ctx, transaction, gameID)
	if err != nil {
		return GameImpact{}, err
	}
	if err := transaction.Commit(); err != nil {
		return GameImpact{}, fmt.Errorf("payloadrelease/impact commit: %w", err)
	}
	return result, nil
}

func GameDeleteImpactTx(ctx context.Context, transaction *sql.Tx, gameID string) (GameImpact, error) {
	blobs, err := gameImpactBlobIDs(ctx, transaction, gameID)
	if err != nil {
		return GameImpact{}, err
	}
	result := GameImpact{SourceKinds: []string{}}
	registered, exclusive, err := impactBytes(ctx, transaction, gameID, uniqueStrings(blobs))
	if err != nil {
		return GameImpact{}, err
	}
	result.RegisteredBytes = strconv.FormatInt(registered, 10)
	result.ExclusiveBytes = strconv.FormatInt(exclusive, 10)
	result.SharedBytes = strconv.FormatInt(registered-exclusive, 10)
	result.BlobCount = int64(len(uniqueStrings(blobs)))
	if err := impactCounts(ctx, transaction, gameID, &result); err != nil {
		return GameImpact{}, err
	}
	result.SourceKinds, err = impactSourceKinds(ctx, transaction, gameID)
	if err != nil {
		return GameImpact{}, err
	}
	canonical := impactCanonical{
		RegisteredBytes: result.RegisteredBytes, ExclusiveBytes: result.ExclusiveBytes,
		SharedBytes: result.SharedBytes, BlobCount: result.BlobCount,
		SaveStateCount: result.SaveStateCount, AssetCount: result.AssetCount,
		ContentFileCount: result.ContentFileCount, ActiveLaunchCount: result.ActiveLaunchCount,
		ActiveNetplayCount: result.ActiveNetplayCount, ReviewEventCount: result.ReviewEventCount,
		SourceKinds: result.SourceKinds,
	}
	encoded, _ := json.Marshal(canonical)
	digest := sha256.Sum256(encoded)
	result.ImpactDigest = hex.EncodeToString(digest[:])
	return result, nil
}

func gameImpactBlobIDs(ctx context.Context, transaction *sql.Tx, gameID string) ([]string, error) {
	ids, err := gameBlobIDs(ctx, transaction, gameID)
	if err != nil {
		return nil, err
	}
	importItems, err := collectIDs(ctx, transaction, `
SELECT source_ref_id FROM game_content_revisions WHERE game_id=? AND source_kind='IMPORT_REVIEW'
UNION SELECT source_ref_id FROM game_metadata_revisions WHERE game_id=? AND source_kind='IMPORT_REVIEW'
`, gameID, gameID)
	if err != nil {
		return nil, err
	}
	for _, itemID := range uniqueStrings(importItems) {
		itemIDs, itemErr := importItemBlobIDs(ctx, transaction, itemID)
		if itemErr != nil {
			return nil, itemErr
		}
		ids = append(ids, itemIDs...)
	}
	pegasusIDs, err := collectIDs(ctx, transaction, `
SELECT source_ref_id FROM game_content_revisions WHERE game_id=? AND source_kind='SERVER_PEGASUS_IMPORT'
UNION SELECT source_ref_id FROM game_metadata_revisions WHERE game_id=? AND source_kind='SERVER_PEGASUS_IMPORT'
`, gameID, gameID)
	if err != nil {
		return nil, err
	}
	for _, itemID := range uniqueStrings(pegasusIDs) {
		values, itemErr := collectIDs(ctx, transaction, `
SELECT blob_id FROM pegasus_import_item_files WHERE item_id=?
UNION ALL SELECT source_archive_blob_id FROM pegasus_import_item_files WHERE item_id=?
UNION ALL SELECT blob_id FROM pegasus_import_item_assets WHERE item_id=?
`, itemID, itemID, itemID)
		if itemErr != nil {
			return nil, itemErr
		}
		ids = append(ids, values...)
	}
	return uniqueStrings(ids), nil
}

func impactBytes(ctx context.Context, transaction *sql.Tx, gameID string, blobIDs []string) (int64, int64, error) {
	var registered, exclusive int64
	for _, blobID := range blobIDs {
		var size int64
		if err := transaction.QueryRowContext(
			ctx, `SELECT size_bytes FROM blobs WHERE id=?`, blobID,
		).Scan(&size); err != nil {
			return 0, 0, fmt.Errorf("payloadrelease/impact blob size: %w", err)
		}
		registered += size
		global, err := globalReferenceCount(ctx, transaction, blobID)
		if err != nil {
			return 0, 0, err
		}
		scoped, err := gameReferenceCount(ctx, transaction, gameID, blobID)
		if err != nil {
			return 0, 0, err
		}
		if global <= scoped {
			exclusive += size
		}
	}
	return registered, exclusive, nil
}

func globalReferenceCount(ctx context.Context, transaction *sql.Tx, blobID string) (int64, error) {
	edges, err := blobregistry.Load()
	if err != nil {
		return 0, fmt.Errorf("payloadrelease/impact registry: %w", err)
	}
	var count int64
	for _, edge := range edges {
		if edge.Class != "PROTECTIVE" {
			continue
		}
		query := `SELECT count(*) FROM "` + edge.Table + `" WHERE "` + edge.Column + `"=?`
		var edgeCount int64
		if err := transaction.QueryRowContext(ctx, query, blobID).Scan(&edgeCount); err != nil {
			return 0, fmt.Errorf("payloadrelease/impact global refs: %w", err)
		}
		count += edgeCount
	}
	return count, nil
}

func impactCounts(ctx context.Context, transaction *sql.Tx, gameID string, result *GameImpact) error {
	err := transaction.QueryRowContext(ctx, `
SELECT
 (SELECT count(*) FROM save_states WHERE game_id=?),
 (SELECT count(*) FROM game_assets WHERE game_id=?),
 (SELECT count(*) FROM game_content_files file
  JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id
  WHERE revision.game_id=?),
 (SELECT count(*) FROM launch_sessions WHERE game_id=? AND state IN ('CREATED','ACTIVE')),
 (SELECT count(*) FROM netplay_sessions WHERE game_id=? AND state NOT IN ('FINISHED','FAILED')),
 (SELECT count(*) FROM review_events WHERE json_extract(after_json,'$.gameId')=?)
`, gameID, gameID, gameID, gameID, gameID, gameID).Scan(
		&result.SaveStateCount, &result.AssetCount, &result.ContentFileCount,
		&result.ActiveLaunchCount, &result.ActiveNetplayCount, &result.ReviewEventCount,
	)
	if err != nil {
		return fmt.Errorf("payloadrelease/impact counts: %w", err)
	}
	return nil
}

func impactSourceKinds(ctx context.Context, transaction *sql.Tx, gameID string) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT source_kind FROM game_content_revisions WHERE game_id=?
UNION SELECT source_kind FROM game_metadata_revisions WHERE game_id=?
ORDER BY source_kind
`, gameID, gameID)
	if err != nil {
		return nil, fmt.Errorf("payloadrelease/impact source kinds: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	set := map[string]struct{}{}
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, fmt.Errorf("payloadrelease/impact source: %w", err)
		}
		switch source {
		case "SERVER_PEGASUS_IMPORT":
			set["SERVER_SCAN"] = struct{}{}
		case "ADMIN_REPLACE":
			set["ADMIN_REPLACE"] = struct{}{}
		case "IMPORT_REVIEW":
			set["USER_UPLOAD"] = struct{}{}
		}
	}
	var result []string
	for source := range set {
		result = append(result, source)
	}
	sort.Strings(result)
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("payloadrelease/impact source rows: %w", err)
	}
	return result, nil
}
