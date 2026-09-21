package blobregistry

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"retrom/internal/cleanup"
)

//go:embed registry.json
var registryBytes []byte

type Edge struct {
	Table  string `json:"table"`
	Column string `json:"column"`
	Target string `json:"target"`
	Class  string `json:"class"`
}

type registry struct {
	SchemaVersion int    `json:"schemaVersion"`
	Edges         []Edge `json:"edges"`
}

var (
	errRegistryInvalid   = errors.New("BLOB_REFERENCE_REGISTRY_INVALID")
	errRegistryDuplicate = errors.New("BLOB_REFERENCE_REGISTRY_DUPLICATE")
	errRegistryMismatch  = errors.New("BLOB_REFERENCE_REGISTRY_MISMATCH")
)

func Load() ([]Edge, error) {
	var value registry
	if err := json.Unmarshal(registryBytes, &value); err != nil || value.SchemaVersion != 1 {
		return nil, errRegistryInvalid
	}
	seen := map[string]struct{}{}
	for _, edge := range value.Edges {
		key := edge.Table + "." + edge.Column
		if edge.Table == "" || edge.Column == "" || edge.Target != "BLOBS" && edge.Target != "ARCHIVE_ENTRY" ||
			edge.Class != "PROTECTIVE" && edge.Class != "ARCHIVE_OWNERSHIP" && edge.Class != "BOOKKEEPING" {
			return nil, errRegistryInvalid
		}
		if _, exists := seen[key]; exists {
			return nil, errRegistryDuplicate
		}
		seen[key] = struct{}{}
	}
	return value.Edges, nil
}

// Every branch validates a distinct generated registry/schema invariant in one audit pass.
func ValidateSchema(ctx context.Context, database *sql.DB) error {
	edges, err := Load()
	if err != nil {
		return err
	}
	expected := map[string]Edge{}
	for _, edge := range edges {
		expected[edge.Table+"."+edge.Column] = edge
	}
	actual, err := schemaBlobReferences(ctx, database)
	if err != nil {
		return err
	}
	problems := compareBlobReferences(expected, actual)
	sort.Strings(problems)
	if len(problems) > 0 {
		return fmt.Errorf("%w: %v", errRegistryMismatch, problems)
	}
	return nil
}

func schemaBlobReferences(ctx context.Context, database *sql.DB) (map[string]string, error) {
	rows, err := database.QueryContext(ctx, `
SELECT name FROM sqlite_schema
WHERE type='table' AND name NOT LIKE 'sqlite_%'
ORDER BY name
`)
	if err != nil {
		return nil, fmt.Errorf("blobregistry/registry: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("blobregistry/registry: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("blobregistry/registry: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("blobregistry/registry: close schema rows: %w", err)
	}
	result := make(map[string]string)
	for _, table := range tables {
		keys, err := foreignKeyTargets(ctx, database, table)
		if err != nil {
			return nil, err
		}
		for key, target := range keys {
			result[key] = target
		}
	}
	return result, nil
}

func compareBlobReferences(expected map[string]Edge, actual map[string]string) []string {
	var problems []string
	for key, edge := range expected {
		target := "blobs.id"
		if edge.Target == "ARCHIVE_ENTRY" {
			target = "archive_entries.archive_blob_id"
		}
		if actual[key] != target {
			problems = append(problems, "registry-only:"+key)
		}
	}
	for key, target := range actual {
		_, exists := expected[key]
		if target == "blobs.id" && !exists {
			problems = append(problems, "schema-only:"+key)
		}
	}
	return problems
}

func foreignKeyTargets(ctx context.Context, database *sql.DB, table string) (map[string]string, error) {
	rows, err := database.QueryContext(ctx, `
PRAGMA foreign_key_list(
`+quoteIdentifier(table)+`)`)
	if err != nil {
		return nil, fmt.Errorf("blobregistry/registry: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := map[string]string{}
	for rows.Next() {
		var id, sequence int
		var targetTable, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &sequence, &targetTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, fmt.Errorf("blobregistry/registry: %w", err)
		}
		result[table+"."+from] = targetTable + "." + to
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("blobregistry/registry: %w", err)
	}
	return result, nil
}

func quoteIdentifier(value string) string {
	result := `"`
	for _, character := range value {
		if character == '"' {
			result += `""`
		} else {
			result += string(character)
		}
	}
	return result + `"`
}
