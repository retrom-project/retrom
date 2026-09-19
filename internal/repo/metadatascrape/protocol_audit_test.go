package metadatascrape

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metadatamodel "retrom/internal/model/metadata"
	metadatascrapemodel "retrom/internal/model/metadatascrape"
	"retrom/internal/testkit/testsupport"
)

func TestProviderProtocolAuditPreservesNullableSQLStatus(t *testing.T) {
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time { return time.UnixMilli(100) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, test := range []struct {
		status int
		audit  metadatamodel.ProtocolAudit
	}{
		{0, nil},
		{200, metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":200}`)},
		{400, metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":400}`)},
		{404, metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":404}`)},
		{429, metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":429}`)},
		{502, metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":502}`)},
	} {
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			id := fmt.Sprintf("response-%d", test.status)
			digest := strings.Repeat("a", 64)
			err := NewRecorder(database.SQL).WithWrite(t.Context(), func(scope metadatascrapemodel.ResultScope) error {
				return scope.Write.Response(t.Context(), metadatascrapemodel.ResponseRecord{
					ID: id, RequestDigest: digest, Outcome: metadatamodel.OutcomeMiss,
					Audit: test.audit, Cacheable: true, Now: 100, ExpiresAt: 200,
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			var stored sql.NullInt64
			if err := database.SQL.QueryRowContext(t.Context(), `SELECT http_status FROM metadata_provider_responses WHERE id=?`, id).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored.Valid != (test.status != 0) || stored.Int64 != int64(test.status) {
				t.Fatalf("SQL audit changed: %+v", stored)
			}
			entry, found, err := NewCache(database.SQL).Cached(t.Context(), digest, 100)
			if err != nil || !found || entry.ID != id || !bytes.Equal(entry.Audit, test.audit) {
				t.Fatalf("cache audit changed: %+v / %t / %v", entry, found, err)
			}
		})
	}
}

func TestProviderProtocolAuditRejectsMalformedEvidence(t *testing.T) {
	t.Parallel()
	var syntax *json.SyntaxError
	_, err := responseAuditStatus(metadatamodel.ProtocolAudit(`{"schemaVersion":`))
	if !errors.As(err, &syntax) {
		t.Fatalf("syntax cause lost: %v", err)
	}
	_, err = responseAuditStatus(metadatamodel.ProtocolAudit(`{"schemaVersion":2,"httpStatus":200}`))
	if !errors.Is(err, errResponseAuditVersion) {
		t.Fatalf("unknown audit version accepted: %v", err)
	}
	status, err := responseAuditStatus(metadatamodel.ProtocolAudit(`{"schemaVersion":1,"httpStatus":0}`))
	if err != nil || status.Valid {
		t.Fatalf("zero status no longer SQL NULL: %v / %v", status, err)
	}
}
