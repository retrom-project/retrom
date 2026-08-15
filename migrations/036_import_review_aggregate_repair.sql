-- Repair import jobs left in REVIEW_PENDING after every review item had already
-- reached PUBLISHED or DISCARDED. Older discard mutations sent the item and job
-- updates as one parameterized multi-statement Exec; the SQLite driver applied
-- the item transition without reliably applying the aggregate transition.

WITH actual AS (
  SELECT
    import_job_id,
    count(*) AS total_count,
    sum(CASE WHEN state='QUEUED' THEN 1 ELSE 0 END) AS queued_count,
    sum(CASE WHEN state IN ('HASHING','IDENTIFYING','SCRAPING') THEN 1 ELSE 0 END) AS running_count,
    sum(CASE WHEN state='REVIEW_PENDING' THEN 1 ELSE 0 END) AS review_pending_count,
    sum(CASE WHEN state='PUBLISHED' THEN 1 ELSE 0 END) AS published_count,
    sum(CASE WHEN state='DISCARDED' THEN 1 ELSE 0 END) AS discarded_count,
    sum(CASE WHEN state IN ('FAILED_RETRYABLE','FAILED_FINAL') THEN 1 ELSE 0 END) AS failed_count,
    sum(CASE WHEN state='CANCELLED' THEN 1 ELSE 0 END) AS cancelled_count,
    max(updated_at_ms) AS latest_item_updated_at_ms,
    max(completed_at_ms) AS latest_item_completed_at_ms
  FROM import_items
  GROUP BY import_job_id
)
UPDATE import_jobs AS job
SET total_item_count=(SELECT total_count FROM actual WHERE import_job_id=job.id),
    queued_item_count=0,
    running_item_count=0,
    review_pending_item_count=0,
    published_item_count=(SELECT published_count FROM actual WHERE import_job_id=job.id),
    discarded_item_count=(SELECT discarded_count FROM actual WHERE import_job_id=job.id),
    failed_item_count=0,
    cancelled_item_count=0,
    state=CASE
      WHEN rejected_file_count=resolved_rejected_file_count THEN 'COMPLETED'
      ELSE 'PARTIAL_FAILURE'
    END,
    version=version+1,
    updated_at_ms=max(
      updated_at_ms,
      (SELECT latest_item_updated_at_ms FROM actual WHERE import_job_id=job.id)
    ),
    completed_at_ms=CASE
      WHEN rejected_file_count=resolved_rejected_file_count THEN coalesce(
        (SELECT latest_item_completed_at_ms FROM actual WHERE import_job_id=job.id),
        updated_at_ms
      )
      ELSE NULL
    END
WHERE job.state='REVIEW_PENDING'
AND job.id IN (
  SELECT import_job_id
  FROM actual
  WHERE total_count>0
  AND queued_count=0
  AND running_count=0
  AND review_pending_count=0
  AND failed_count=0
  AND cancelled_count=0
  AND total_count=published_count+discarded_count
);
