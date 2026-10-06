CREATE SEQUENCE emulator_game_numbers AS BIGINT
  MINVALUE 1 MAXVALUE 9007199254740991 START WITH 1001 NO CYCLE;

SELECT setval('emulator_game_numbers', GREATEST(1000, COALESCE(max(emulator_game_id), 1000)), true)
FROM game_variants;
