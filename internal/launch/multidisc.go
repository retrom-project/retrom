package launch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"

	"retrom/internal/contentcapability"

	"retrom/internal/cleanup"
	"retrom/internal/corevalidation"
	"retrom/internal/multidisc"
)

type lockedDisc struct {
	Index       int
	BlobID      string
	Digest      string
	SizeBytes   int64
	LogicalName string
	VirtualPath string
}

type lockedContentFile struct {
	BlobID      string
	LogicalName string
	Format      string
}

type launchContentPlan struct {
	ContentKind string
	Files       []lockedContentFile
	Discs       []lockedDisc
}

type multiDiscQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (plan launchContentPlan) singleFile() (lockedContentFile, bool) {
	if len(plan.Files) != 1 {
		return lockedContentFile{}, false
	}
	return plan.Files[0], true
}

var (
	errLockedBlobSizeMismatch = fmt.Errorf("locked blob size mismatch")
	errLockedPlaylistMismatch = fmt.Errorf("locked playlist mismatch")
)

func (service *Service) multiDiscRevalidationInputs(
	ctx context.Context,
	database multiDiscQueryer,
	variantID, gameID, providerID, targetID string,
	contentPolicy contentcapability.Policy,
	datID sql.NullString,
	biosSnapshot corevalidation.Snapshot,
) (string, string, corevalidation.Snapshot, error) {
	ordered, canonicalDigest, err := service.multiDiscBlobEvidence(ctx, database, variantID, gameID)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, err
	}
	biosDigest, err := corevalidation.BIOSDependencyDigest(biosSnapshot)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, ErrBlocked
	}
	biosSnapshot.MultiDisc = &corevalidation.MultiDiscSnapshot{
		ContentKind: corevalidation.MultiDiscContentKind, ParserVersion: corevalidation.MultiDiscParserVersion,
		DiscCount: len(ordered), MissingEntries: []corevalidation.MultiDiscMissingEntry{},
		OrderedDiscSHA256: ordered, CanonicalPlaylistSHA256: canonicalDigest,
		Delivery: corevalidation.MultiDiscDelivery,
	}
	digest, err := corevalidation.MultiDiscValidationInputDigest(corevalidation.MultiDiscValidationInput{
		GameVariantID: variantID, GameID: gameID,
		ContentKind: corevalidation.MultiDiscContentKind, ProviderID: providerID, TargetID: targetID,
		ContentPolicySHA256: contentPolicy.Digest(),
		DATVersionID:        datID, BIOSDependencySHA256: biosDigest,
		OrderedDiscSHA256: ordered, CanonicalPlaylistSHA256: canonicalDigest,
	})
	if err != nil {
		return "", "", corevalidation.Snapshot{}, fmt.Errorf("launch/multi-disc revalidation input: %w", err)
	}
	return digest, biosDigest, biosSnapshot, nil
}

func (service *Service) multiDiscBlobEvidence(
	ctx context.Context,
	database multiDiscQueryer,
	variantID, gameID string,
) ([]string, string, error) {
	rows, err := database.QueryContext(ctx, `
SELECT blob.sha256 FROM game_files file
JOIN blobs blob ON blob.id=file.blob_id
WHERE file.game_id=? AND file.role='DISC'
ORDER BY file.sort_order,file.logical_name
`, gameID)
	if err != nil {
		return nil, "", fmt.Errorf("launch/multi-disc evidence: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	ordered := make([]string, 0, multidisc.MaxDiscs)
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			return nil, "", fmt.Errorf("launch/multi-disc evidence: %w", err)
		}
		ordered = append(ordered, digest)
	}
	if err := rows.Err(); err != nil || len(ordered) < multidisc.MinDiscs || len(ordered) > multidisc.MaxDiscs {
		return nil, "", ErrBlocked
	}
	var canonicalDigest string
	if err := database.QueryRowContext(ctx, `
SELECT blob.sha256 FROM variant_files file JOIN blobs blob ON blob.id=file.blob_id
WHERE file.game_variant_id=? AND file.role='MULTI_DISC_PLAYLIST'
 AND file.logical_name='playlist.m3u'
`, variantID).Scan(&canonicalDigest); err != nil {
		return nil, "", ErrBlocked
	}
	return ordered, canonicalDigest, nil
}

func (service *Service) buildMultiDiscLaunchContentPlan(
	ctx context.Context,
	variantID, contentID, snapshotJSON string,
) (launchContentPlan, error) {
	snapshot, err := corevalidation.ParseSnapshot(snapshotJSON)
	if err != nil || snapshot.MultiDisc == nil || len(snapshot.MultiDisc.MissingEntries) != 0 {
		return launchContentPlan{}, ErrBlocked
	}
	var playlistBlobID, playlistDigest string
	var playlistSize int64
	if err := service.database.QueryRowContext(ctx, `
SELECT file.blob_id,blob.sha256,blob.size_bytes
FROM variant_files file JOIN blobs blob ON blob.id=file.blob_id
WHERE file.game_variant_id=? AND file.role='MULTI_DISC_PLAYLIST'
 AND file.logical_name='playlist.m3u'
`, variantID).Scan(&playlistBlobID, &playlistDigest, &playlistSize); err != nil {
		return launchContentPlan{}, ErrBlocked
	}
	discs, canonical, err := service.readLockedMultiDiscs(ctx, contentID, 1_073_741_824)
	if err != nil {
		return launchContentPlan{}, err
	}
	if !validLockedMultiDiscEvidence(
		snapshot.MultiDisc, discs, canonical, playlistDigest, playlistSize, multidisc.MaxDiscs,
	) {
		return launchContentPlan{}, ErrBlocked
	}
	if service.blobs != nil && !service.verifyLockedMultiDiscBlobs(playlistDigest, playlistSize, canonical, discs) {
		return launchContentPlan{}, ErrBlocked
	}
	return launchContentPlan{
		ContentKind: multidisc.ContentKind,
		Files: []lockedContentFile{{
			BlobID: playlistBlobID, LogicalName: "playlist.m3u", Format: "RETROM_MULTIDISC_M3U_V1",
		}},
		Discs: discs,
	}, nil
}

func (service *Service) readLockedMultiDiscs(
	ctx context.Context,
	contentID string,
	maximumTotalBytes int64,
) ([]lockedDisc, []byte, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT file.blob_id,blob.sha256,blob.size_bytes,file.sort_order
FROM game_files file JOIN blobs blob ON blob.id=file.blob_id
WHERE file.game_id=? AND file.role='DISC'
ORDER BY file.sort_order,file.logical_name
`, contentID)
	if err != nil {
		return nil, nil, fmt.Errorf("launch/multi-disc content: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	discs := make([]lockedDisc, 0, multidisc.MaxDiscs)
	var totalSize int64
	canonical := make([]byte, 0, multidisc.MaxDiscs*13)
	for rows.Next() {
		var disc lockedDisc
		var sortOrder int
		if err := rows.Scan(&disc.BlobID, &disc.Digest, &disc.SizeBytes, &sortOrder); err != nil {
			return nil, nil, fmt.Errorf("launch/multi-disc content: %w", err)
		}
		disc.Index = len(discs)
		if sortOrder != disc.Index || disc.SizeBytes < 8 || disc.SizeBytes > maximumTotalBytes-totalSize {
			return nil, nil, ErrBlocked
		}
		disc.LogicalName = fmt.Sprintf("disc-%03d.chd", disc.Index+1)
		disc.VirtualPath = "/" + disc.LogicalName
		canonical = append(canonical, disc.LogicalName...)
		canonical = append(canonical, '\n')
		totalSize += disc.SizeBytes
		discs = append(discs, disc)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("launch/multi-disc content: %w", err)
	}
	return discs, canonical, nil
}

func validLockedMultiDiscEvidence(
	snapshot *corevalidation.MultiDiscSnapshot,
	discs []lockedDisc,
	canonical []byte,
	playlistDigest string,
	playlistSize int64,
	maximumDiscs int,
) bool {
	canonicalSHA := sha256.Sum256(canonical)
	canonicalDigest := hex.EncodeToString(canonicalSHA[:])
	if snapshot == nil || len(discs) < multidisc.MinDiscs || len(discs) > maximumDiscs ||
		len(discs) != snapshot.DiscCount || playlistSize != int64(len(canonical)) ||
		playlistDigest != canonicalDigest || playlistDigest != snapshot.CanonicalPlaylistSHA256 ||
		len(snapshot.OrderedDiscSHA256) != len(discs) {
		return false
	}
	for index := range discs {
		if discs[index].Digest != snapshot.OrderedDiscSHA256[index] {
			return false
		}
	}
	return true
}

func (service *Service) verifyLockedMultiDiscBlobs(
	playlistDigest string,
	playlistSize int64,
	canonical []byte,
	discs []lockedDisc,
) bool {
	if err := service.verifyLockedBlob(playlistDigest, playlistSize, canonical); err != nil {
		return false
	}
	for _, disc := range discs {
		if err := service.verifyLockedBlob(disc.Digest, disc.SizeBytes, nil); err != nil {
			return false
		}
	}
	return true
}

func (service *Service) verifyLockedBlob(digest string, size int64, exact []byte) error {
	file, err := service.blobs.OpenDigest(digest)
	if err != nil {
		return fmt.Errorf("open locked blob: %w", err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	stat, err := file.Stat()
	if err != nil || stat.Size() != size {
		return errLockedBlobSizeMismatch
	}
	if exact == nil {
		return nil
	}
	actual, err := io.ReadAll(io.LimitReader(file, int64(len(exact))+1))
	if err != nil || !bytes.Equal(actual, exact) {
		return errLockedPlaylistMismatch
	}
	return nil
}
