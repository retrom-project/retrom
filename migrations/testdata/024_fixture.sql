PRAGMA defer_foreign_keys=ON;
BEGIN;

INSERT INTO profiles(id,display_name,created_at_ms) VALUES
  ('01980000-0000-7000-8000-00000000a101','Migration 024 Admin',2000),
  ('01980000-0000-7000-8000-00000000a102','Migration 024 Player',2000);

INSERT INTO users(
  id,profile_id,username,display_name,role,status,session_version,version,created_at_ms,updated_at_ms
) VALUES
  ('01980000-0000-7000-8000-00000000b101','01980000-0000-7000-8000-00000000a101',
   'migration024.admin','Migration 024 Admin','ADMIN','ENABLED',1,1,2000,2000),
  ('01980000-0000-7000-8000-00000000b102','01980000-0000-7000-8000-00000000a102',
   'migration024.player','Migration 024 Player','USER','ENABLED',1,1,2000,2000);

INSERT INTO user_credentials(user_id,password_hash,password_scheme,password_changed_at_ms,created_at_ms)
VALUES
  ('01980000-0000-7000-8000-00000000b101','fixture-024-admin','ARGON2ID_V1',2000,2000),
  ('01980000-0000-7000-8000-00000000b102','fixture-024-player','ARGON2ID_V1',2000,2000);

UPDATE instance_state
SET state='COMPLETED',bootstrap_kind='RELEASE_SETUP',
    initial_admin_user_id='01980000-0000-7000-8000-00000000b101',
    version=2,updated_at_ms=2000,initialized_at_ms=2000
WHERE id=1;

INSERT INTO idempotency_records(
  principal_id,operation_id,key,request_digest,http_status,response_headers_json,response_body,
  created_at_ms,expires_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000b101','fixture-024-operation',
  '01980000-0000-7000-8000-00000000b199',
  '5555555555555555555555555555555555555555555555555555555555555555',
  200,'{}',x'7b7d',2000,4000
);

COMMIT;
