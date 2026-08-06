package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"retrom/internal/cleanup"
)

type streamEvent struct {
	ID   int64
	Type string
	Data string
}

func (server *Server) streamJobEvents(writer http.ResponseWriter, request *http.Request, jobID string) {
	cursor, maximum, snapshot, ok := server.jobStreamSnapshot(writer, request, jobID)
	if !ok {
		return
	}
	server.streamEvents(writer, request, cursor, maximum, snapshot,
		func(ctx context.Context, after int64) ([]streamEvent, error) {
			return server.readJobEvents(ctx, jobID, after)
		},
		func(ctx context.Context) (bool, error) {
			var state string
			err := server.database.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state)
			if err != nil {
				return false, fmt.Errorf("read job stream state: %w", err)
			}
			return state == "SUCCEEDED" || state == "FAILED" || state == "CANCELLED", nil
		})
}

func (server *Server) streamAggregateEvents(writer http.ResponseWriter, request *http.Request, importJobID string) {
	cursor, maximum, snapshot, ok := server.importStreamSnapshot(writer, request, importJobID)
	if !ok {
		return
	}
	server.streamEvents(writer, request, cursor, maximum, snapshot,
		func(ctx context.Context, after int64) ([]streamEvent, error) {
			rows, err := server.database.QueryContext(
				ctx,
				`
SELECT e.id,
e.event_type,
e.data_json
FROM job_events e
WHERE e.id>?
AND ((e.scope_type='IMPORT_GROUP'
AND e.scope_id=?)
OR (e.scope_type='IMPORT_ITEM'
AND EXISTS(SELECT 1
FROM import_items item
WHERE item.id=e.scope_id
AND item.import_job_id=?)))
ORDER BY e.id LIMIT 1000
`,
				after,
				importJobID,
				importJobID,
			)
			return scanStreamEvents(rows, err)
		},
		func(ctx context.Context) (bool, error) {
			var state string
			err := server.database.QueryRowContext(ctx, `
SELECT state
FROM import_jobs
WHERE id=?
`, importJobID).
				Scan(&state)
			if err != nil {
				return false, fmt.Errorf("read import stream state: %w", err)
			}
			return state == "COMPLETED" || state == "FAILED" || state == "CANCELLED" ||
				state == "REVIEW_PENDING", nil
		})
}

func (server *Server) jobStreamSnapshot(
	writer http.ResponseWriter,
	request *http.Request,
	jobID string,
) (int64, int64, []byte, bool) {
	transaction, err := server.database.BeginTx(request.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		server.databaseError(writer, request, err)
		return 0, 0, nil, false
	}
	defer cleanup.Rollback(transaction)
	var maximum int64
	if err := transaction.QueryRowContext(request.Context(), `
SELECT COALESCE(MAX(id),
0)
FROM job_events
`).Scan(&maximum); err != nil {
		server.databaseError(writer, request, err)
		return 0, 0, nil, false
	}
	var state string
	var version int64
	var errorCode sql.NullString
	if err := transaction.QueryRowContext(request.Context(), `
SELECT state,
version,
error_code
FROM jobs
WHERE id=?
`, jobID).Scan(&state, &version, &errorCode); errors.Is(
		err,
		sql.ErrNoRows,
	) {
		server.notFound(writer, request)
		return 0, 0, nil, false
	} else if err != nil {
		server.databaseError(writer, request, err)
		return 0, 0, nil, false
	}
	cursor, valid := parseEventCursor(request, maximum)
	if !valid {
		writeError(writer, request, http.StatusBadRequest, "INVALID_EVENT_CURSOR", "事件游标无效", map[string]any{})
		return 0, 0, nil, false
	}
	snapshot, _ := json.Marshal(
		map[string]any{"errorCode": nullableString(errorCode), "jobId": jobID, "state": state, "version": version},
	)
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return 0, 0, nil, false
	}
	return cursor, maximum, snapshot, true
}

func (server *Server) importStreamSnapshot(
	writer http.ResponseWriter,
	request *http.Request,
	importJobID string,
) (int64, int64, []byte, bool) {
	transaction, err := server.database.BeginTx(request.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		server.databaseError(writer, request, err)
		return 0, 0, nil, false
	}
	defer cleanup.Rollback(transaction)
	var maximum int64
	if err := transaction.QueryRowContext(request.Context(), `
SELECT COALESCE(MAX(id),
0)
FROM job_events
`).Scan(&maximum); err != nil {
		server.databaseError(writer, request, err)
		return 0, 0, nil, false
	}
	var state string
	var version, total, queued, running, reviewPending, failed int64
	if err := transaction.QueryRowContext(request.Context(), `
SELECT state,
version,
total_item_count,
queued_item_count,
running_item_count,
review_pending_item_count,
failed_item_count
FROM import_jobs
WHERE id=?
`, importJobID).Scan(&state, &version, &total, &queued, &running, &reviewPending, &failed); errors.Is(
		err,
		sql.ErrNoRows,
	) {
		server.notFound(writer, request)
		return 0, 0, nil, false
	} else if err != nil {
		server.databaseError(writer, request, err)
		return 0, 0, nil, false
	}
	cursor, valid := parseEventCursor(request, maximum)
	if !valid {
		writeError(writer, request, http.StatusBadRequest, "INVALID_EVENT_CURSOR", "事件游标无效", map[string]any{})
		return 0, 0, nil, false
	}
	snapshot, _ := json.Marshal(
		map[string]any{
			"importJobId":            importJobID,
			"state":                  state,
			"version":                version,
			"totalItemCount":         total,
			"queuedItemCount":        queued,
			"runningItemCount":       running,
			"reviewPendingItemCount": reviewPending,
			"failedItemCount":        failed,
		},
	)
	if err := transaction.Commit(); err != nil {
		server.databaseError(writer, request, err)
		return 0, 0, nil, false
	}
	return cursor, maximum, snapshot, true
}

func parseEventCursor(request *http.Request, maximum int64) (int64, bool) {
	raw := request.Header.Get("Last-Event-ID")
	if raw == "" {
		return maximum, true
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	return parsed, err == nil && parsed >= 0 && parsed <= maximum && strconv.FormatInt(parsed, 10) == raw
}

func (server *Server) readJobEvents(ctx context.Context, jobID string, after int64) ([]streamEvent, error) {
	rows, err := server.database.QueryContext(
		ctx,
		`
SELECT id,
event_type,
data_json
FROM job_events
WHERE job_id=?
AND id>?
ORDER BY id LIMIT 1000
`,
		jobID,
		after,
	)
	return scanStreamEvents(rows, err)
}

func scanStreamEvents(rows *sql.Rows, err error) ([]streamEvent, error) {
	if err != nil {
		return nil, err
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]streamEvent, 0)
	for rows.Next() {
		var item streamEvent
		if err := rows.Scan(&item.ID, &item.Type, &item.Data); err != nil {
			return nil, fmt.Errorf("httpapi/sse: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan stream events: %w", err)
	}
	return items, nil
}

func (server *Server) streamEvents(
	writer http.ResponseWriter,
	request *http.Request,
	cursor, snapshotID int64,
	snapshot []byte,
	read func(context.Context, int64) ([]streamEvent, error),
	terminal func(context.Context) (bool, error),
) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(
			writer,
			request,
			http.StatusInternalServerError,
			"STREAMING_UNAVAILABLE",
			"服务器不支持事件流",
			map[string]any{},
		)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("X-Accel-Buffering", "no")
	if request.Header.Get("Last-Event-ID") == "" {
		// snapshot is JSON-encoded by the server, so embedded CR/LF bytes are escaped before reaching SSE framing.
		if _, err := fmt.Fprintf( //nolint:gosec // Server-marshaled JSON escapes CR/LF bytes.
			writer,
			"id: %d\nevent: snapshot\ndata: %s\n\n",
			snapshotID,
			snapshot,
		); err != nil {
			return
		}
		flusher.Flush()
	}
	poll := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(server.sseHeartbeat)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		events, err := read(request.Context(), cursor)
		if err != nil {
			return
		}
		for _, event := range events {
			if _, err := fmt.Fprintf(
				writer,
				"id: %d\nevent: %s\ndata: %s\n\n",
				event.ID,
				strings.ToLower(event.Type),
				event.Data,
			); err != nil {
				return
			}
			cursor = event.ID
		}
		if len(events) > 0 {
			flusher.Flush()
		}
		done, err := terminal(request.Context())
		if err != nil || done {
			return
		}
		select {
		case <-request.Context().Done():
			return
		case <-poll.C:
		case <-heartbeat.C:
			if _, err := fmt.Fprint(writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
