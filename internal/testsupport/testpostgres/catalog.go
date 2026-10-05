package testpostgres

// SchemaObjectsSQL describes application objects through PostgreSQL's catalogs.
// Constraint-created indexes are included; internal foreign-key triggers are not.
const SchemaObjectsSQL = `
 SELECT c.relname AS name,'table'::text AS type,
  coalesce((SELECT string_agg(a.attname || ' ' || format_type(a.atttypid,a.atttypmod),',' ORDER BY a.attnum)
   FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped),'') || ' ' ||
  coalesce((SELECT string_agg(pg_get_constraintdef(k.oid),',' ORDER BY k.conname)
   FROM pg_constraint k WHERE k.conrelid=c.oid),'') AS sql
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema() AND c.relkind='r'
 UNION ALL SELECT indexname,'index',indexdef FROM pg_indexes WHERE schemaname=current_schema()
 UNION ALL SELECT viewname,'view',definition FROM pg_views WHERE schemaname=current_schema()
 UNION ALL SELECT t.tgname,'trigger',pg_get_triggerdef(t.oid)
 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema() AND NOT t.tgisinternal
`
