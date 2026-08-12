ALTER TABLE pegasus_import_items
ADD COLUMN error_details_json TEXT
CHECK(
  error_details_json IS NULL OR (
    json_valid(error_details_json)
    AND json_type(error_details_json)='object'
    AND length(CAST(error_details_json AS BLOB))<=8192
  )
);

-- Migration 028 handed every explicitly declared ZIP in the same mapped Arcade
-- directory to one library-import item. Once that set exceeded the library
-- source limit, the only persisted result was PEGASUS_LIBRARY_IMPORT_FAILED.
-- Preserve an exact diagnostic for those already-terminal items so an admin can
-- understand and retry the affected plan after upgrading.
UPDATE pegasus_import_items AS item
SET error_details_json=json_object(
  'schemaVersion',1,
  'stage','LIBRARY_IMPORT',
  'operation','CREATE_SERVER_SOURCE',
  'causeCode','SOURCE_FILE_LIMIT_EXCEEDED',
  'technicalDetail',printf(
    'Pegasus assembled %d source files for one Arcade item; library import accepts at most 64.',
    1+(
      SELECT count(*)
      FROM pegasus_import_items candidate
      JOIN pegasus_import_collections candidate_collection ON candidate_collection.id=candidate.collection_id
      JOIN pegasus_import_item_files candidate_file ON candidate_file.item_id=candidate.id
      WHERE candidate.import_id=item.import_id
      AND candidate.id<>item.id
      AND candidate.discovery_state='READY'
      AND candidate_collection.mapping_action='IMPORT'
      AND candidate_collection.target_platform_instance_id=(
        SELECT own_collection.target_platform_instance_id
        FROM pegasus_import_collections own_collection
        WHERE own_collection.id=item.collection_id
      )
      AND (SELECT count(*) FROM pegasus_import_item_files own_file WHERE own_file.item_id=candidate.id)=1
      AND lower(substr(candidate_file.relative_path,-4))='.zip'
    )
  ),
  'relativePath',(
    SELECT own_file.relative_path
    FROM pegasus_import_item_files own_file
    WHERE own_file.item_id=item.id
    ORDER BY own_file.ordinal
    LIMIT 1
  ),
  'observedFileCount',1+(
    SELECT count(*)
    FROM pegasus_import_items candidate
    JOIN pegasus_import_collections candidate_collection ON candidate_collection.id=candidate.collection_id
    JOIN pegasus_import_item_files candidate_file ON candidate_file.item_id=candidate.id
    WHERE candidate.import_id=item.import_id
    AND candidate.id<>item.id
    AND candidate.discovery_state='READY'
    AND candidate_collection.mapping_action='IMPORT'
    AND candidate_collection.target_platform_instance_id=(
      SELECT own_collection.target_platform_instance_id
      FROM pegasus_import_collections own_collection
      WHERE own_collection.id=item.collection_id
    )
    AND (SELECT count(*) FROM pegasus_import_item_files own_file WHERE own_file.item_id=candidate.id)=1
    AND lower(substr(candidate_file.relative_path,-4))='.zip'
  ),
  'allowedFileCount',64,
  'libraryImportJobId',NULL,
  'libraryImportItemId',NULL
)
WHERE item.execution_state='COMMIT_FAILED'
AND item.error_code='PEGASUS_LIBRARY_IMPORT_FAILED'
AND item.library_import_job_id IS NULL
AND item.library_import_item_id IS NULL
AND EXISTS(
  SELECT 1
  FROM pegasus_import_collections own_collection
  WHERE own_collection.id=item.collection_id
  AND own_collection.target_platform_id='arcade'
)
AND (SELECT count(*) FROM pegasus_import_item_files own_file WHERE own_file.item_id=item.id)=1
AND lower((
  SELECT substr(own_file.relative_path,-4)
  FROM pegasus_import_item_files own_file
  WHERE own_file.item_id=item.id
  LIMIT 1
))='.zip'
AND 1+(
  SELECT count(*)
  FROM pegasus_import_items candidate
  JOIN pegasus_import_collections candidate_collection ON candidate_collection.id=candidate.collection_id
  JOIN pegasus_import_item_files candidate_file ON candidate_file.item_id=candidate.id
  WHERE candidate.import_id=item.import_id
  AND candidate.id<>item.id
  AND candidate.discovery_state='READY'
  AND candidate_collection.mapping_action='IMPORT'
  AND candidate_collection.target_platform_instance_id=(
    SELECT own_collection.target_platform_instance_id
    FROM pegasus_import_collections own_collection
    WHERE own_collection.id=item.collection_id
  )
  AND (SELECT count(*) FROM pegasus_import_item_files own_file WHERE own_file.item_id=candidate.id)=1
  AND lower(substr(candidate_file.relative_path,-4))='.zip'
)>64;
