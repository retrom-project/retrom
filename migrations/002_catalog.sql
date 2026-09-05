-- Pre-release bootstrap: create the current domain model directly.

CREATE TABLE platforms (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  sort_order INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms)
);

CREATE TABLE cores (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms)
);

CREATE TABLE content_kinds (
  id TEXT PRIMARY KEY CHECK(
    length(id) BETWEEN 2 AND 64 AND id=upper(id) AND id NOT GLOB '*[^A-Z0-9_]*'
  )
);

CREATE TABLE platform_cores (
  platform_id TEXT NOT NULL REFERENCES platforms(id),
  core_id TEXT NOT NULL REFERENCES cores(id),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  PRIMARY KEY(platform_id, core_id)
);

CREATE TABLE runtime_providers (
  provider_id TEXT PRIMARY KEY CHECK(
    length(provider_id) BETWEEN 1 AND 64 AND provider_id=lower(provider_id)
    AND provider_id NOT GLOB '*[^a-z0-9-]*'
  ),
  provider_version TEXT NOT NULL CHECK(length(provider_version) BETWEEN 5 AND 128),
  provider_api_version INTEGER NOT NULL CHECK(provider_api_version>=1),
  bundle_sha256 TEXT NOT NULL UNIQUE CHECK(length(bundle_sha256)=64 AND bundle_sha256=lower(bundle_sha256)),
  manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256=lower(manifest_sha256)),
  module_sha256 TEXT NOT NULL CHECK(length(module_sha256)=64 AND module_sha256=lower(module_sha256)),
  source TEXT NOT NULL CHECK(source IN ('candidate','production')),
  release_repository TEXT,
  release_tag TEXT,
  release_commit TEXT,
  activated_at_ms INTEGER NOT NULL CHECK(activated_at_ms>=0),
  UNIQUE(provider_id,bundle_sha256),
  CHECK(
    source='candidate' AND release_repository IS NULL AND release_tag IS NULL AND release_commit IS NULL
    OR source='production' AND release_repository IS NOT NULL AND release_tag IS NOT NULL
      AND length(release_commit)=40 AND release_commit=lower(release_commit)
  )
);

CREATE TABLE runtime_catalog_state (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1),
  catalog_sha256 TEXT NOT NULL CHECK(length(catalog_sha256)=64 AND catalog_sha256=lower(catalog_sha256)),
  activated_at_ms INTEGER NOT NULL CHECK(activated_at_ms>=0)
);

CREATE TABLE runtime_target_bindings (
  binding_id TEXT PRIMARY KEY CHECK(
    length(binding_id) BETWEEN 1 AND 128 AND binding_id=lower(binding_id)
    AND binding_id NOT GLOB '*[^a-z0-9-]*'
  ),
  core_id TEXT NOT NULL REFERENCES cores(id),
  provider_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  detector_profile TEXT NOT NULL CHECK(length(detector_profile) BETWEEN 2 AND 64),
  delivery_profile TEXT NOT NULL CHECK(length(delivery_profile) BETWEEN 2 AND 64),
  launch_policy TEXT NOT NULL CHECK(
    length(launch_policy) BETWEEN 2 AND 64 AND launch_policy=upper(launch_policy)
    AND launch_policy NOT GLOB '*[^A-Z0-9_]*'
  ),
  review_policy TEXT NOT NULL CHECK(
    length(review_policy) BETWEEN 2 AND 64 AND review_policy=upper(review_policy)
    AND review_policy NOT GLOB '*[^A-Z0-9_]*'
  ),
  UNIQUE(provider_id,target_id),
  FOREIGN KEY(provider_id,target_id) REFERENCES runtime_targets(provider_id,target_id) ON DELETE CASCADE
);

CREATE TABLE runtime_binding_platforms (
  binding_id TEXT NOT NULL REFERENCES runtime_target_bindings(binding_id) ON DELETE CASCADE,
  platform_id TEXT NOT NULL,
  core_id TEXT NOT NULL,
  PRIMARY KEY(binding_id,platform_id),
  FOREIGN KEY(platform_id,core_id) REFERENCES platform_cores(platform_id,core_id)
);

CREATE TABLE runtime_binding_content_kinds (
  binding_id TEXT NOT NULL REFERENCES runtime_target_bindings(binding_id) ON DELETE CASCADE,
  content_kind TEXT NOT NULL REFERENCES content_kinds(id),
  PRIMARY KEY(binding_id,content_kind)
);

CREATE TABLE platform_instances (
  id TEXT PRIMARY KEY,
  platform_id TEXT NOT NULL,
  default_core_id TEXT NOT NULL,
  name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
  slug TEXT NOT NULL CHECK(slug = lower(slug) AND slug NOT LIKE '-%' AND slug NOT LIKE '%-' AND slug NOT LIKE '%--%'),
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version >= 1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms >= 0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms >= created_at_ms),
  deleted_at_ms INTEGER CHECK(deleted_at_ms IS NULL OR deleted_at_ms >= created_at_ms), catalog_template_key TEXT
CHECK (
  catalog_template_key IS NULL OR (
    length(catalog_template_key) BETWEEN 3 AND 160
    AND catalog_template_key = lower(catalog_template_key)
    AND catalog_template_key NOT GLOB '*[^a-z0-9_/-]*'
    AND catalog_template_key GLOB '*/*'
    AND catalog_template_key NOT GLOB '*/*/*'
  )
),
  UNIQUE(platform_id, slug),
  UNIQUE(id, platform_id),
  FOREIGN KEY(platform_id, default_core_id) REFERENCES platform_cores(platform_id, core_id)
);

CREATE TABLE "runtime_targets" (
  provider_id TEXT NOT NULL REFERENCES runtime_providers(provider_id) ON DELETE CASCADE,
  target_id TEXT NOT NULL CHECK(
    length(target_id) BETWEEN 1 AND 64 AND target_id=lower(target_id)
    AND target_id NOT GLOB '*[^a-z0-9-]*'
  ),
  display_name TEXT NOT NULL CHECK(length(display_name) BETWEEN 1 AND 120),
  target_options_schema_json TEXT NOT NULL CHECK(
    json_valid(target_options_schema_json) AND json_type(target_options_schema_json)='object'
  ),
  capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)),
  checkpoint_json TEXT CHECK(checkpoint_json IS NULL OR json_valid(checkpoint_json)),
  manifest_fragment_json TEXT NOT NULL CHECK(json_valid(manifest_fragment_json)),
  PRIMARY KEY(provider_id,target_id)
);
