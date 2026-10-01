-- BIOS library membership starts from the required Provider/Target, then checks
-- the candidate games' current publication state.
CREATE INDEX game_variants_provider_target_game
ON game_variants(provider_id,target_id,game_id);
