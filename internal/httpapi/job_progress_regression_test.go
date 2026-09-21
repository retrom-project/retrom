package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const progressJobID = "01980000-0000-7000-8000-000000000091"

func TestTerminalJobStreamDrainsAllBatches(t *testing.T) {
	server := newTestServer(t)
	seedProgressJob(t, server, "SUCCEEDED")
	transaction, err := server.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 1001 {
		if _, err := transaction.ExecContext(t.Context(), `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'GAME_VARIANT',?,'PROGRESS','{}',1)`, progressJobID, progressJobID); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	writer := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.Header.Set("Last-Event-ID", "0")
	server.streamJobEvents(writer, request, progressJobID)
	if count := strings.Count(writer.Body.String(), "event: progress\n"); count != 1001 {
		t.Fatalf("terminal stream lost backlog: got %d events, want 1001", count)
	}
}

func TestJobSnapshotMatchesDetail(t *testing.T) {
	server := newTestServer(t)
	seedProgressJob(t, server, "SUCCEEDED")
	detailRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	detailRequest.SetPathValue("jobId", progressJobID)
	detail := httptest.NewRecorder()
	server.job(detail, detailRequest)
	stream := httptest.NewRecorder()
	server.streamJobEvents(stream, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), progressJobID)
	var want, got map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &want); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(stream.Body.String(), "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			if err := json.Unmarshal([]byte(data), &got); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SSE detail diverges: got=%v want=%v", got, want)
	}
}

func TestJobStreamIncludesEventCommittedWhileWritingBatch(t *testing.T) {
	server := newTestServer(t)
	seedProgressJob(t, server, "RUNNING")
	appendProgressEvent(t, server, "STARTED")
	writer := &progressRaceWriter{ResponseRecorder: httptest.NewRecorder(), commit: func() {
		appendProgressEvent(t, server, "SUCCEEDED")
		if _, err := server.database.ExecContext(t.Context(), `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=2 WHERE id=?`, progressJobID); err != nil {
			t.Fatal(err)
		}
	}}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.Header.Set("Last-Event-ID", "0")
	server.streamJobEvents(writer, request, progressJobID)
	if !strings.Contains(writer.Body.String(), "event: succeeded\n") {
		t.Fatalf("stream closed before final event: %s", writer.Body.String())
	}
}

func seedProgressJob(t *testing.T, server *Server, state string) {
	t.Helper()
	var finished *int64
	if state == "SUCCEEDED" {
		now := int64(1)
		finished = &now
	}
	if _, err := server.database.ExecContext(t.Context(), `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'GAME_VARIANT',?,'VARIANT_VALIDATE',?,1,'{}',0,?,1,2,1,?,1,1)`,
		progressJobID, progressJobID, strings.Repeat("9", 64), state, finished); err != nil {
		t.Fatal(err)
	}
}

func appendProgressEvent(t *testing.T, server *Server, event string) {
	t.Helper()
	if _, err := server.database.ExecContext(t.Context(), `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'GAME_VARIANT',?,?,'{}',1)`, progressJobID, progressJobID, event); err != nil {
		t.Fatal(err)
	}
}

type progressRaceWriter struct {
	*httptest.ResponseRecorder
	commit func()
}

func (writer *progressRaceWriter) Write(data []byte) (int, error) {
	n, err := writer.ResponseRecorder.Write(data)
	if writer.commit != nil {
		commit := writer.commit
		writer.commit = nil
		commit()
	}
	return n, err
}
