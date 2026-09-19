package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/content/multidisc"
	model "retrom/internal/model/launch"
)

func ProductValidationInputs(snapshot model.ProductSnapshot, variantID string) (model.ValidationInputs, error) {
	source := snapshot.Source
	bios, _, _, err := ResolveProductBIOS(source, snapshot.ValidationBIOS, source.ValidationLogicalName)
	if err != nil {
		return model.ValidationInputs{}, err
	}
	digest, biosDigest, _, err := ProductValidationEvidence(snapshot, variantID, bios)
	if err != nil {
		return model.ValidationInputs{}, err
	}
	return model.ValidationInputs{
		GameID: source.GameID, GameVariantID: variantID, GameVersion: source.GameVersion,
		SourceManifestDigest: source.SourceManifestDigest, ProviderID: source.ProviderID, TargetID: source.TargetID,
		ContentPolicy: source.ContentPolicy, DATVersionID: source.ActiveDATVersionID,
		ValidationInputDigest: BindCurrentGameStateDigest(
			digest,
			source.GameVersion,
			source.SourceManifestDigest,
		), BIOSDependencyDigest: biosDigest,
	}, nil
}

func ProductValidationEvidence(
	snapshot model.ProductSnapshot,
	variantID string,
	bios corevalidation.Snapshot,
) (string, string, corevalidation.Snapshot, error) {
	source := snapshot.Source
	if source.ContentKind == multidisc.ContentKind {
		return productDiscValidationInputs(snapshot, variantID, bios)
	}
	digest, err := corevalidation.ProviderValidationInputDigest(
		source.ProviderID,
		source.TargetID,
		source.GameID,
		source.ActiveDATVersionID,
		bios,
	)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, fmt.Errorf("digest product validation input: %w", err)
	}
	encoded, err := bios.JSON()
	if err != nil {
		return "", "", corevalidation.Snapshot{}, fmt.Errorf("encode validation BIOS: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return digest, hex.EncodeToString(hash[:]), bios, nil
}

func productDiscValidationInputs(
	snapshot model.ProductSnapshot,
	variantID string,
	bios corevalidation.Snapshot,
) (string, string, corevalidation.Snapshot, error) {
	ordered := make([]string, 0, multidisc.MaxDiscs)
	for _, file := range snapshot.GameFiles {
		if file.Role == "DISC" {
			ordered = append(ordered, file.Digest)
		}
	}
	playlist, found := productFile(snapshot.VariantFiles, "MULTI_DISC_PLAYLIST", "playlist.m3u")
	if !found || len(ordered) < multidisc.MinDiscs || len(ordered) > multidisc.MaxDiscs {
		return "", "", corevalidation.Snapshot{}, model.ErrBlocked
	}
	biosDigest, err := corevalidation.BIOSDependencyDigest(bios)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, fmt.Errorf("digest validation disc BIOS: %w", err)
	}
	source := snapshot.Source
	digest, err := corevalidation.MultiDiscValidationInputDigest(corevalidation.MultiDiscValidationInput{
		GameVariantID: variantID, GameID: source.GameID, ContentKind: multidisc.ContentKind,
		ProviderID: source.ProviderID, TargetID: source.TargetID,
		ContentPolicySHA256: source.ContentPolicy.Digest(), DATVersionID: source.ActiveDATVersionID,
		BIOSDependencySHA256: biosDigest,
		OrderedDiscSHA256:    ordered, CanonicalPlaylistSHA256: playlist.Digest,
	})
	if err != nil {
		return "", "", corevalidation.Snapshot{}, fmt.Errorf("digest product disc input: %w", err)
	}
	bios.MultiDisc = &corevalidation.MultiDiscSnapshot{
		ContentKind: corevalidation.MultiDiscContentKind, ParserVersion: corevalidation.MultiDiscParserVersion,
		DiscCount: len(ordered), MissingEntries: []corevalidation.MultiDiscMissingEntry{}, OrderedDiscSHA256: ordered,
		CanonicalPlaylistSHA256: playlist.Digest, Delivery: corevalidation.MultiDiscDelivery,
	}
	return digest, biosDigest, bios, nil
}
