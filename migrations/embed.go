package migrations

import _ "embed"

// Schema creates the current application schema in a new database.
//
//go:embed 001_schema.sql
var Schema string

// QueryIndexes adds pagination and active-reference indexes without changing domain facts.
//
//go:embed 002_query_indexes.sql
var QueryIndexes string

// ContentPackages adds immutable STORE ZIP member indexes to existing databases.
//
//go:embed 003_content_packages.sql
var ContentPackages string

// TitleOrder adds letter groups, pinyin sort keys and admin sort names to games.
//
//go:embed 004_title_order.sql
var TitleOrder string
