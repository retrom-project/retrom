package gamecontent

import (
	"context"
	"fmt"

	"retrom/internal/persistence/blobcatalog"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/service/gamecontent"
)

func (writes writes) rpgProfile(ctx context.Context, value gamecontent.Publication) error {
	gameID, profile := value.Snapshot.GameID, value.Prepared.RPGMaker
	if _, err := writes.transaction.ExecContext(
		ctx,
		`DELETE FROM rpgmaker_game_profiles WHERE game_id=?`,
		gameID,
	); err != nil {
		return fmt.Errorf("replace RPG content profile: %w", err)
	}
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
	_, err := recordstore.CreateRpgmakerGameProfiles(ctx, writes.transaction, `INSERT INTO rpgmaker_game_profiles(
 game_id,evidence_family,evidence_generation,evidence_confidence,engine_version,entry_html_path,file_count,total_bytes,
 project_fingerprint,requirements_sha256,analysis_json,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		gameID, profile.Profile.EvidenceFamily, generation, profile.Profile.EvidenceConfidence, engine, entryHTML,
		profile.FileCount, profile.TotalBytes,
		profile.ProjectFingerprint, profile.RequirementsSHA256, string(profile.AnalysisJSON), value.Now, value.Now)
	if err != nil {
		return fmt.Errorf("write replacement RPG profile: %w", err)
	}
	return nil
}

func (writes writes) rpgVariant(ctx context.Context, value gamecontent.Publication) error {
	snapshot := value.Snapshot
	err := requireChanged(
		writes.transaction.ExecContext(
			ctx,
			`UPDATE rpgmaker_variant_profiles
 SET generation=?,dependency_snapshot_sha256=? WHERE game_variant_id=?`,
			snapshot.RPGGeneration,
			snapshot.RPGDependencySHA256,
			snapshot.VariantID,
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
