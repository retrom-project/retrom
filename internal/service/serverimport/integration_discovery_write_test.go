package serverimport_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"testing"

	serversourcecontract "retrom/internal/adapter/files/serversource"
	servermodel "retrom/internal/model/serverimport"
	serverimportcontract "retrom/internal/service/serverimport"

	"retrom/internal/capability/content/firmware"
)

func discoveryWriteFixture(t *testing.T) (*serverimportcontract.Service, *sql.DB, servermodel.Work, *serverimportcontract.EvaluatedCandidate) {
	t.Helper()
	service, database, _ := archiveImportFixture(t)
	created, err := service.Create(t.Context(), servermodel.CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "bios-root"}, controlActorID)
	if err != nil {
		t.Fatal(err)
	}
	unit, found, err := service.ClaimForTest(t.Context())
	if err != nil || !found {
		t.Fatalf("claim: %v %v", found, err)
	}
	items, err := service.LoadItemsForTest(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	candidate := &serverimportcontract.EvaluatedCandidate{ID: "discovery-candidate", Item: items[0], File: serversourcecontract.File{RelativePath: "fixture.zip", Basename: "fixture.zip", SizeBytes: 1}, Association: "EXACT_NAME", State: "INELIGIBLE", Details: map[string]any{"code": "INVALID_ARCHIVE"}}
	return service, database, unit, candidate
}

func TestDiscoveryRejectsUnencodableEvidenceWithoutPartialWrites(t *testing.T) {
	service, database, unit, candidate := discoveryWriteFixture(t)
	candidate.Details["bad"] = math.NaN()
	err := service.PersistCandidatesForTest(t.Context(), unit, map[string][]*serverimportcontract.EvaluatedCandidate{candidate.Item.RequirementID: {candidate}}, servermodel.DiscoveryCounts{})
	var encodingErr *json.UnsupportedValueError
	if !errors.As(err, &encodingErr) {
		t.Errorf("unencodable evidence: %v", err)
	}
	var candidates int64
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM server_bios_import_candidates WHERE server_import_id=?`, unit.ImportID).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if candidates != 0 {
		t.Fatalf("invalid evidence committed %d candidates", candidates)
	}
}

func TestDiscoveryPreservesUnsafeArchiveEvidence(t *testing.T) {
	service, database, unit, candidate := discoveryWriteFixture(t)
	candidate.State = "ARCHIVE_UNSAFE"
	candidate.DAT = &firmware.DATEvaluation{SafeArchive: false, Launchable: false}
	if err := service.PersistCandidatesForTest(t.Context(), unit, map[string][]*serverimportcontract.EvaluatedCandidate{candidate.Item.RequirementID: {candidate}}, servermodel.DiscoveryCounts{}); err != nil {
		t.Fatal(err)
	}
	var safe bool
	if err := database.QueryRowContext(t.Context(), `SELECT safe_archive FROM server_bios_import_candidates WHERE id=?`, candidate.ID).Scan(&safe); err != nil {
		t.Fatal(err)
	}
	if safe {
		t.Fatal("unsafe archive evidence persisted as safe")
	}
}
