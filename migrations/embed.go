package migrations

import _ "embed"

// Schema creates the application schema in a new database. Later schema changes are appended as numbered files
// starting at 002.
//
//go:embed 001_schema.sql
var Schema string
