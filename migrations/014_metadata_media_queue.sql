-- Media order and byte accounting are durable domain facts; jobs own execution state.
CREATE TABLE metadata_media_runs (
  scrape_run_id TEXT PRIMARY KEY REFERENCES metadata_scrape_runs(id) ON DELETE CASCADE,
  order_frozen_at_ms INTEGER CHECK(order_frozen_at_ms IS NULL OR order_frozen_at_ms>=0),
  charged_bytes INTEGER NOT NULL DEFAULT 0 CHECK(charged_bytes BETWEEN 0 AND 104857600),
  version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>=0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms)
);

CREATE UNIQUE INDEX scrape_candidate_assets_media_job ON scrape_candidate_assets(media_fetch_job_id)
  WHERE media_fetch_job_id IS NOT NULL;
CREATE INDEX scrape_candidate_assets_media_order ON scrape_candidate_assets(scrape_candidate_id,media_fetch_order);
