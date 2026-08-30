DROP TRIGGER game_variant_revisions_runtime_insert;

CREATE TRIGGER game_variant_revisions_runtime_insert
BEFORE INSERT ON game_variant_revisions
WHEN NOT EXISTS(
  SELECT 1 FROM game_variants variant
  JOIN core_artifacts artifact ON artifact.id=NEW.core_artifact_id
  WHERE variant.id=NEW.game_variant_id AND artifact.core_id=variant.core_id
    AND artifact.route_key=NEW.route_key AND artifact.available_for_launch=1
    AND (
      artifact.runtime_family='EMULATORJS'
        AND (NEW.status!='READY' OR NEW.emulator_game_id IS NOT NULL)
      OR artifact.runtime_family='RPGMAKER' AND NEW.emulator_game_id IS NULL
      OR artifact.runtime_family='ONS' AND NEW.emulator_game_id IS NULL
      OR artifact.runtime_family='KIRIKIRI' AND NEW.emulator_game_id IS NULL
      OR artifact.runtime_family='BUTTERSCOTCH' AND NEW.emulator_game_id IS NULL
    )
)
BEGIN SELECT RAISE(ABORT,'variant revision runtime mismatch'); END;
