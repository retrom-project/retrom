//go:build integration

package httpapi

import (
	"io"
	"testing"

	dbapi "retrom/internal/database"
)

func assertOwnedGameFile(t *testing.T, server *Server, gameID, fileID string) {
	t.Helper()
	var retained bool
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT EXISTS(SELECT 1 FROM stored_files WHERE id=? AND owner_kind='GAME' AND owner_id=? AND retired_at_ms IS NULL)`, fileID, gameID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if !retained {
		t.Fatal("other game's file ownership changed")
	}
	file, err := server.blobs.OpenID(fileID)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	contents, err := io.ReadAll(file)
	if err != nil || string(contents) != "move-game" {
		t.Fatalf("other game's bytes: %q %v", contents, err)
	}
}

func assertRetiredGameFile(t *testing.T, server *Server, fileID string) {
	t.Helper()
	var retained bool
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT EXISTS(SELECT 1 FROM stored_files WHERE id=? AND retired_at_ms IS NULL)`, fileID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained {
		t.Fatal("deleted game's file was not retired")
	}
}
