-- NES and FDS share the same default runtime family, while the two MAME 2003
-- seed directories accept the same Arcade payload format. Keep the canonical
-- destination rows, move existing games without rewriting their immutable
-- content/variant/save history, and retain the retired rows as soft-deleted
-- tombstones for historical import and audit foreign keys.

UPDATE platform_instances
SET enabled=CASE WHEN deleted_at_ms IS NULL THEN enabled ELSE 1 END,
    version=version+CASE WHEN deleted_at_ms IS NULL THEN 0 ELSE 1 END,
    updated_at_ms=CASE
      WHEN deleted_at_ms IS NULL THEN updated_at_ms
      ELSE max(updated_at_ms,COALESCE((SELECT max(applied_at_ms) FROM schema_migrations),updated_at_ms))
    END,
    deleted_at_ms=NULL
WHERE id IN (
  '01980000-0000-7000-8000-000000000001',
  '01980000-0000-7000-8000-000000000007'
);

UPDATE games
SET platform_instance_id=CASE platform_instance_id
      WHEN '01980000-0000-7000-8000-000000000002' THEN '01980000-0000-7000-8000-000000000001'
      WHEN '01980000-0000-7000-8000-000000000008' THEN '01980000-0000-7000-8000-000000000007'
    END,
    version=version+1,
    updated_at_ms=max(updated_at_ms,COALESCE((SELECT max(applied_at_ms) FROM schema_migrations),updated_at_ms))
WHERE platform_instance_id IN (
  '01980000-0000-7000-8000-000000000002',
  '01980000-0000-7000-8000-000000000008'
);

UPDATE platform_instances
SET enabled=0,
    version=version+CASE WHEN deleted_at_ms IS NULL OR enabled<>0 THEN 1 ELSE 0 END,
    updated_at_ms=CASE
      WHEN deleted_at_ms IS NULL OR enabled<>0
      THEN max(updated_at_ms,COALESCE((SELECT max(applied_at_ms) FROM schema_migrations),updated_at_ms))
      ELSE updated_at_ms
    END,
    deleted_at_ms=COALESCE(
      deleted_at_ms,
      max(updated_at_ms,COALESCE((SELECT max(applied_at_ms) FROM schema_migrations),updated_at_ms))
    )
WHERE id IN (
  '01980000-0000-7000-8000-000000000002',
  '01980000-0000-7000-8000-000000000008'
);
