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
