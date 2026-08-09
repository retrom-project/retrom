UPDATE import_jobs
SET state=CASE
    WHEN rejected_file_count>0 THEN 'PARTIAL_FAILURE'
    ELSE 'COMPLETED'
  END,
  completed_at_ms=CASE
    WHEN rejected_file_count=0 THEN updated_at_ms
    ELSE NULL
  END,
  version=version+1
WHERE state IN ('RUNNING','REVIEW_PENDING')
AND total_item_count=0
AND queued_item_count=0
AND running_item_count=0
AND review_pending_item_count=0;
