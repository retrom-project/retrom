package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	gamevariant "retrom/internal/service/gamevariant"

	corevalidation "retrom/internal/core/validation"
	"retrom/internal/multidisc"
)

func productMultiDiscContent(snapshot ProductSnapshot) (ProductContent, error) {
	locked, err := corevalidation.ParseSnapshot(snapshot.Source.DependencySnapshot)
	if err != nil || locked.MultiDisc == nil || len(locked.MultiDisc.MissingEntries) != 0 {
		return ProductContent{}, ErrBlocked
	}
	playlist, found := productFile(snapshot.VariantFiles, "MULTI_DISC_PLAYLIST", "playlist.m3u")
	if !found {
		return ProductContent{}, ErrBlocked
	}
	discs, canonical, err := productDiscs(snapshot.GameFiles)
	if err != nil {
		return ProductContent{}, err
	}
	if !validProductMultiDiscEvidence(locked.MultiDisc, discs, canonical, playlist) {
		return ProductContent{}, ErrBlocked
	}
	checks := []ProductBlobCheck{
		{FileRecord: playlist.FileRecord, Digest: playlist.Digest, SizeBytes: playlist.SizeBytes, Exact: canonical},
	}
	for _, disc := range discs {
		checks = append(checks, ProductBlobCheck{FileRecord: disc.FileRecord, Digest: disc.Digest, SizeBytes: disc.SizeBytes})
	}
	return ProductContent{
		Files: []ProductContentFile{
			{FileRecord: playlist.FileRecord, LogicalName: "playlist.m3u", Format: "RETROM_MULTIDISC_M3U_V1"},
		},
		Discs:  discs,
		Checks: checks,
	}, nil
}

func productDiscs(files []gamevariant.File) ([]ProductDisc, []byte, error) {
	discs := make([]ProductDisc, 0, multidisc.MaxDiscs)
	canonical := make([]byte, 0, multidisc.MaxDiscs*13)
	var total int64
	for _, file := range files {
		if file.Role != "DISC" {
			continue
		}
		index := len(discs)
		if file.SortOrder != index || file.SizeBytes < 8 || file.SizeBytes > 1_073_741_824-total {
			return nil, nil, ErrBlocked
		}
		name := fmt.Sprintf("disc-%03d.chd", index+1)
		discs = append(
			discs,
			ProductDisc{
				Index:       index,
				FileRecord:  file.FileRecord,
				Digest:      file.Digest,
				SizeBytes:   file.SizeBytes,
				LogicalName: name,
				VirtualPath: "/" + name,
			},
		)
		canonical = append(canonical, name...)
		canonical = append(canonical, '\n')
		total += file.SizeBytes
	}
	return discs, canonical, nil
}

func validProductMultiDiscEvidence(
	snapshot *corevalidation.MultiDiscSnapshot,
	discs []ProductDisc,
	canonical []byte,
	playlist gamevariant.File,
) bool {
	hash := sha256.Sum256(canonical)
	digest := hex.EncodeToString(hash[:])
	if snapshot == nil || len(
		discs,
	) < multidisc.MinDiscs || len(
		discs,
	) > multidisc.MaxDiscs || len(
		discs,
	) != snapshot.DiscCount ||
		playlist.SizeBytes != int64(
			len(canonical),
		) || playlist.Digest != digest || playlist.Digest != snapshot.CanonicalPlaylistSHA256 || len(
		snapshot.OrderedDiscSHA256,
	) != len(
		discs,
	) {
		return false
	}
	for index, disc := range discs {
		if disc.Digest != snapshot.OrderedDiscSHA256[index] {
			return false
		}
	}
	return true
}
