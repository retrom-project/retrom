DROP TRIGGER rpgmaker_variant_profiles_validate_insert;

CREATE TRIGGER rpgmaker_variant_profiles_validate_insert
BEFORE INSERT ON rpgmaker_variant_profiles
WHEN NOT (
  NEW.runtime_validation_id IS NOT NULL AND EXISTS(
    SELECT 1 FROM game_variant_revisions revision
    JOIN game_variants variant ON variant.id=revision.game_variant_id
    JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
    JOIN rpgmaker_core_generations mapping
      ON mapping.core_id=variant.core_id AND mapping.generation=NEW.generation
    JOIN rpgmaker_content_profiles content ON content.content_revision_id=revision.game_content_revision_id
    JOIN rpgmaker_runtime_validations validation ON validation.id=NEW.runtime_validation_id
    WHERE revision.id=NEW.game_variant_revision_id AND revision.status='READY'
      AND artifact.runtime_family='RPGMAKER' AND artifact.route_key=revision.route_key
      AND artifact.available_for_launch=1
      AND NEW.route_key=revision.route_key AND NEW.adapter_id=artifact.adapter_id
      AND NEW.artifact_set_sha256=artifact.artifact_set_sha256
      AND (content.evidence_generation=NEW.generation OR
        content.evidence_family='RPG2K' AND content.evidence_confidence='FAMILY_ONLY'
          AND content.evidence_generation IS NULL AND NEW.generation IN ('RPG2000','RPG2003'))
      AND validation.launch_id IS NOT NULL AND validation.core_id=variant.core_id
      AND validation.generation=NEW.generation AND validation.route_key=NEW.route_key
      AND validation.artifact_id=artifact.id
      AND validation.artifact_set_sha256=NEW.artifact_set_sha256
      AND validation.adapter_id=NEW.adapter_id AND validation.adapter_abi=NEW.adapter_abi
      AND validation.dependency_snapshot_sha256=NEW.dependency_snapshot_sha256
      AND validation.project_fingerprint=content.project_fingerprint
  )
  OR NEW.runtime_validation_id IS NULL AND EXISTS(
    SELECT 1 FROM game_variant_revisions revision
    JOIN game_content_revisions content_revision
      ON content_revision.id=revision.game_content_revision_id
      AND content_revision.source_kind='ADMIN_REPLACE'
    JOIN game_variants variant ON variant.id=revision.game_variant_id
    JOIN core_artifacts artifact ON artifact.id=revision.core_artifact_id
    JOIN rpgmaker_core_generations mapping
      ON mapping.core_id=variant.core_id AND mapping.generation=NEW.generation
    JOIN rpgmaker_content_profiles content
      ON content.content_revision_id=revision.game_content_revision_id
      AND content.evidence_confidence='MATCHED'
      AND content.evidence_generation=NEW.generation
    WHERE revision.id=NEW.game_variant_revision_id AND revision.status='READY'
      AND artifact.runtime_family='RPGMAKER' AND artifact.route_key=revision.route_key
      AND artifact.available_for_launch=1
      AND NEW.route_key=revision.route_key AND NEW.adapter_id=artifact.adapter_id
      AND NEW.artifact_set_sha256=artifact.artifact_set_sha256
  )
)
BEGIN SELECT RAISE(ABORT,'invalid RPG Maker variant profile'); END;
