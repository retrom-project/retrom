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
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT EXISTS(SELECT 1 FROM game_files WHERE file_record=? AND game_id=?)`, fileID, gameID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if !retained {
		t.Fatal("other game's file ownership changed")
	}
	file, err := server.blobs.OpenRecord(fileID)
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
	if err := dbapi.QueryRowContext(t.Context(), server.database, `SELECT EXISTS(SELECT 1 FROM game_files WHERE file_record=?)`, fileID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained {
		t.Fatal("deleted game's file was not retired")
	}
}
