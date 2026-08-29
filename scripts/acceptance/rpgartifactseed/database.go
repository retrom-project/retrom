package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"retrom/internal/dependencies"
	"retrom/internal/store"
)

type databaseHandle struct {
	store *store.DB
}

func openDatabase(ctx context.Context, path string, now clock) (*databaseHandle, error) {
	database, err := store.Open(ctx, path, now)
	if err != nil {
		return nil, fmt.Errorf("open acceptance database through store: %w", err)
	}
	return &databaseHandle{store: database}, nil
}

func (database *databaseHandle) close() error { return database.store.Close() }

func (database *databaseHandle) sql() *sql.DB { return database.store.SQL }

func (database *databaseHandle) integrityCheck(ctx context.Context) error {
	return database.store.IntegrityCheck(ctx)
}

func bootstrapDependencies(
	ctx context.Context,
	database *sql.DB,
	dependencyRoot string,
	versions []string,
	active string,
	now time.Time,
) error {
	set, err := dependencies.Load(dependencyRoot, versions, active)
	if err != nil {
		return fmt.Errorf("load byte-verified dependencies: %w", err)
	}
	if err := set.Bootstrap(ctx, database, now); err != nil {
		return fmt.Errorf("bootstrap byte-verified dependencies: %w", err)
	}
	return nil
}

func requireFreshBusinessDatabase(ctx context.Context, database *sql.DB) error {
	tables := []string{
		"upload_sessions", "import_jobs", "games", "save_states",
		"rpgmaker_runtime_validations", "runtime_asset_pack_installations",
	}
	for _, table := range tables {
		var count int64
		query := "SELECT count(*) FROM " + table // table names are a closed constant list.
		if err := database.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fmt.Errorf("count %s: %w", table, err)
		}
		if count != 0 {
			return fmt.Errorf("ACC_RPG_012_DATABASE_NOT_FRESH: %s=%d", table, count)
		}
	}
	return nil
}

func splitVersions(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	versions := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		version := strings.TrimSpace(part)
		if version == "" || strings.ContainsAny(version, "/\\") {
			return nil, errors.New("ACC_RPG_012_DEPENDENCY_VERSIONS_INVALID")
		}
		if _, duplicate := seen[version]; duplicate {
			return nil, errors.New("ACC_RPG_012_DEPENDENCY_VERSIONS_DUPLICATE")
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		return nil, errors.New("ACC_RPG_012_DEPENDENCY_VERSIONS_EMPTY")
	}
	return versions, nil
}
