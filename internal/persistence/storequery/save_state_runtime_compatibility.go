package storequery

// SaveRuntimeCompatibility is the shared relational projection used by application queries.
const SaveRuntimeCompatibility = `
SELECT save.id AS save_state_id,
CASE WHEN EXISTS(
  SELECT 1 FROM game_variants variant
  JOIN runtime_targets target ON target.provider_id=variant.provider_id AND target.target_id=variant.target_id
  WHERE variant.game_id=save.game_id AND variant.status='READY'
    AND target.checkpoint_json IS NOT NULL
    AND EXISTS(
      SELECT 1 FROM jsonb_array_elements(((target.checkpoint_json)::jsonb #> '{readFormats}')) readable
      WHERE jsonb_typeof(readable.value)='string' AND (readable.value #>> '{}')=save.checkpoint_format
    )
) THEN 'AVAILABLE' ELSE 'INCOMPATIBLE_RUNTIME' END AS status
FROM save_states save
`
