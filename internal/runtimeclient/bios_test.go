package runtimeclient

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBiosCatalogPreservesArchiveMembersForIdentification(t *testing.T) {
	const catalog = `[{"requirementKey":"provider/core/firmware.zip","logicalName":"firmware.zip",
		"members":[{"name":"boot.bin","sizeBytes":8192,"crc32":"12345678","sha1":"abcdef","required":true},
		{"name":"optional.bin","sizeBytes":256,"crc32":"87654321","sha1":"","required":false}]}]`
	var client Client
	if err := json.Unmarshal([]byte(catalog), &client.BiosRequirements); err != nil {
		t.Fatal(err)
	}
	requirements, err := client.AllBios(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]any{"requirements": requirements})
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Requirements []map[string]json.RawMessage `json:"requirements"`
	}
	if err = json.Unmarshal(input, &sent); err != nil {
		t.Fatal(err)
	}
	var original []map[string]json.RawMessage
	if err = json.Unmarshal([]byte(catalog), &original); err != nil {
		t.Fatal(err)
	}
	var expected, actual []map[string]any
	if err = json.Unmarshal(original[0]["members"], &expected); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(sent.Requirements[0]["members"], &actual); err != nil {
		t.Fatalf("BIOS identification lost the catalog's ZIP members: %s (%v)", input, err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("BIOS member facts changed before identification: got %v, want %v", actual, expected)
	}
}
