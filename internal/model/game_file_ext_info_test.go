package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOriginalAttachmentOwnershipAndPrivateJSON(t *testing.T) {
	gameID, fileID := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	key, err := OriginStorageKey(gameID, fileID)
	if err != nil {
		t.Fatal(err)
	}
	file := GameFile{ID: fileID, Role: "project_package", ExtInfo: GameFileTabExtInfo{
		ConversionInfo: &GameFileConversionInfo{OriginInfo: &OriginGameInfo{
			StorageKey: key, LogicalKey: "原包.zip", SizeBytes: 123, SHA256: strings.Repeat("a", 64),
		}},
	}}
	if err = ValidateGameFileExtInfo(gameID, file); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(file.ExtInfo)
	if err != nil || !strings.Contains(string(encoded), `"conversion_info":{"origin_info":`) || strings.Contains(string(encoded), "fingerprint") {
		t.Fatalf("extension JSON=%s err=%v", encoded, err)
	}
	encoded, err = json.Marshal(file)
	if err != nil || strings.Contains(string(encoded), "origin") || strings.Contains(string(encoded), "conversion") || strings.Contains(string(encoded), gameID) {
		t.Fatalf("private attachment exposed: %s err=%v", encoded, err)
	}
	for _, bad := range []string{
		"games/" + gameID + "/origins/" + fileID + "/../original",
		strings.Replace(key, gameID, fileID, 1), key + "/extra", strings.ToUpper(key),
	} {
		file.ExtInfo.ConversionInfo.OriginInfo.StorageKey = bad
		if ValidateGameFileExtInfo(gameID, file) == nil {
			t.Fatalf("invalid attachment ownership accepted: %q", bad)
		}
	}
}

func TestOriginPathParserRequiresExactCanonicalLayout(t *testing.T) {
	gameID, fileID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	key, err := OriginStorageKey(gameID, fileID)
	if err != nil {
		t.Fatal(err)
	}
	game, file, ok := ParseOriginStorageKey(key)
	if !ok || game != gameID || file != fileID {
		t.Fatal("canonical attachment path rejected")
	}
	for _, bad := range []string{key + "/extra", strings.ToUpper(key), "games/../origins/" + fileID + "/original", "games/" + gameID + "/origins/bad/original"} {
		if _, _, ok = ParseOriginStorageKey(bad); ok {
			t.Fatalf("malformed attachment protected: %q", bad)
		}
	}
}
