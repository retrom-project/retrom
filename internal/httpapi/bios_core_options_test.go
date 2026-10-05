package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

func TestBIOSCoreOptionsIgnoreFiltersAndPagination(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	for _, coreID := range []string{"mgba", "gambatte"} {
		requireHTTPTestRuntimeTarget(t, server.database, coreID)
		target, err := testsupport.LookupRuntimeTarget(t.Context(), server.database, coreID)
		testassert.False(t, err != nil, err)
		_, err = server.database.ExecContext(t.Context(), `
INSERT INTO bios_requirements(id,core_id,provider_id,target_id,source_kind,logical_name,
requirement_mode,catalog_digest,size_bytes,md5,source_url,source_version,enabled,version,
created_at_ms,updated_at_ms,delivery_kind)
VALUES(?,?,?,?, 'STATIC',?,'REQUIRED',lower(upper(encode(decode(repeat('00',(32)::integer),'hex'),'hex'))),1,lower(upper(encode(decode(repeat('00',(16)::integer),'hex'),'hex'))),
'https://example.invalid/bios','options-v1',1,1,1,1,'BIOS_BUNDLE')`,
			coreID, coreID, target.ProviderID, target.TargetID, coreID+".bin")
		testassert.False(t, err != nil, err)
	}
	for _, query := range []string{"limit=1", "coreId=mgba", "q=no-match", "status=MATCHED", "quick=OPTIONAL"} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"/api/v1/admin/bios?scope=FULL_CATALOG&"+query, nil)
			server.bios(response, request)
			testassert.Falsef(t, response.Code != http.StatusOK, "list = %d %s", response.Code, response.Body.String())
			var body struct {
				CoreOptions []struct{ ID, Name string } `json:"coreOptions"`
			}
			err := json.Unmarshal(response.Body.Bytes(), &body)
			testassert.False(t, err != nil, err)
			testassert.Falsef(t, len(body.CoreOptions) != 2, "core options = %+v", body.CoreOptions)
			testassert.Falsef(t, body.CoreOptions[0].ID != "gambatte", "first core = %+v", body.CoreOptions[0])
			testassert.Falsef(t, body.CoreOptions[1].ID != "mgba", "second core = %+v", body.CoreOptions[1])
		})
	}
}
