package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"retrom/internal/filestore"
	"retrom/internal/testassert"
)

func TestAdminGameFilesExposeStoredMetadataForEveryRole(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	gameID := "01980000-0000-7000-8000-000000000141"
	seedFavoriteHTTPGame(t, server, gameID, "", "File details")
	roles := []string{"CONTENT", "DISC", "COMPANION", "PROJECT_FILE"}
	want := make([]map[string]any, 0, len(roles))
	for index, role := range roles {
		file, err := server.contentDeps.Files.Put(bytes.NewBufferString(role))
		testassert.False(t, err != nil, err)
		file, err = server.contentDeps.Files.CopyTo(t.Context(), file.Record,
			filestore.GameDirectory(gameID)+"/content", role)
		testassert.False(t, err != nil, err)
		record, err := filestore.ParseRecord(file.Record)
		testassert.False(t, err != nil, err)
		mustCreateHTTPReferences(t, server.database, "game_files", `
INSERT INTO game_files(game_id,role,logical_name,file_record,sort_order) VALUES(?,?,?,?,?)
`, gameID, role, role+".bin", file.Record, index)
		want = append(want, map[string]any{
			"role": role, "logicalName": role + ".bin", "sortOrder": float64(index),
			"sizeBytes": float64(record.Size), "sha256": record.SHA256, "md5": record.MD5,
			"sha1": record.SHA1, "crc32": record.CRC32, "mediaType": record.MediaType,
		})
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(),
		http.MethodGet, "/api/v1/admin/games/"+gameID, nil))
	testassert.Falsef(t, response.Code != http.StatusOK, "status %d: %s", response.Code, response.Body.String())
	var body struct {
		Files []map[string]any `json:"files"`
	}
	mustDecodeHTTPTest(t, response.Body.Bytes(), &body)
	actualJSON, err := json.Marshal(body.Files)
	testassert.False(t, err != nil, err)
	wantJSON, err := json.Marshal(want)
	testassert.False(t, err != nil, err)
	testassert.Falsef(t, string(actualJSON) != string(wantJSON), "files = %s; want %s", actualJSON, wantJSON)
}
