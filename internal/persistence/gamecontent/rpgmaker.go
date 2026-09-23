package gamecontent

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/blobcatalog"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/profilemodel"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/service/gamecontent"
)

func (writes writes) rpgProfile(ctx context.Context, value gamecontent.Publication) error {
	gameID, profile := value.Snapshot.GameID, value.Prepared.RPGMaker
	var generation, engine, entryHTML *string
	if profile.Profile.EvidenceGeneration != nil {
		value := string(*profile.Profile.EvidenceGeneration)
		generation = &value
	}
	if profile.Profile.EngineVersion != "" {
		engine = &profile.Profile.EngineVersion
	}
	if profile.Profile.ExpectedGeneration == detector.RPGMV || profile.Profile.ExpectedGeneration == detector.RPGMZ {
		value := "index.html"
		entryHTML = &value
	}
	encoded, err := profilemodel.Encode(profilemodel.Game, profilemodel.RPGMakerProject, &profilemodel.RPGGame{
		EvidenceFamily: profile.Profile.EvidenceFamily, EvidenceGeneration: generation,
		EvidenceConfidence: string(profile.Profile.EvidenceConfidence), EngineVersion: engine,
		EntryHTMLPath: entryHTML, FileCount: profile.FileCount, TotalBytes: profile.TotalBytes,
		ProjectFingerprint: profile.ProjectFingerprint, RequirementsSHA256: profile.RequirementsSHA256,
		Analysis: json.RawMessage(profile.AnalysisJSON),
	})
	if err != nil {
		return fmt.Errorf("encode replacement RPG profile: %w", err)
	}
	err = requireChanged(writes.transaction.ExecContext(ctx, `UPDATE games SET content_profile_json=?
WHERE id=? AND content_kind='RPG_MAKER_PROJECT'
 AND json_extract(source_manifest_json,'$.fileCount')=?
 AND json_extract(source_manifest_json,'$.totalBytes')=?
 AND json_extract(source_manifest_json,'$.filesDigest')=?`,
		encoded, gameID,
		profile.FileCount, profile.TotalBytes, profile.ProjectFingerprint))
	if err != nil {
		return fmt.Errorf("write replacement RPG profile: %w", err)
	}
	return nil
}

func (writes writes) rpgVariant(ctx context.Context, value gamecontent.Publication) error {
	snapshot := value.Snapshot
	encoded, err := profilemodel.Encode(profilemodel.Variant, profilemodel.RPGMakerProject,
		&profilemodel.RPGVariant{
			Generation: snapshot.RPGGeneration, DependencySnapshotSHA256: snapshot.RPGDependencySHA256,
		})
	if err != nil {
		return fmt.Errorf("encode replacement RPG variant profile: %w", err)
	}
	err = requireChanged(
		writes.transaction.ExecContext(
			ctx,
			`UPDATE game_variants SET runtime_profile_json=? WHERE id=? AND runtime_profile_json IS NOT NULL`,
			encoded, snapshot.VariantID,
		),
	)
	if err != nil {
		return err
	}
	for index, file := range value.Prepared.RPGMaker.VariantFiles {
		id, err := blobcatalog.EnsureRecord(ctx, writes.transaction, file.Metadata, "application/octet-stream", value.Now)
		if err != nil {
			return fmt.Errorf("register replacement RPG file: %w", err)
		}
		_, err = recordstore.CreateVariantFiles(
			ctx,
			writes.transaction,
			`INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
  VALUES(?,?,?,?,?)`,
			snapshot.VariantID,
			file.Role,
			file.LogicalName,
			id,
			index,
		)
		if err != nil {
			return fmt.Errorf("attach replacement RPG file: %w", err)
		}
	}
	return nil
}
